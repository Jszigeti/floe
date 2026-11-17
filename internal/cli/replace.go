package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Jszigeti/floe/internal/apps"
	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/inventory"
	"github.com/Jszigeti/floe/internal/keychain"
	"github.com/Jszigeti/floe/internal/launchd"
	"github.com/Jszigeti/floe/internal/manifest"
	"github.com/Jszigeti/floe/internal/replace"
	"github.com/Jszigeti/floe/internal/restic"
	"github.com/Jszigeti/floe/internal/run"
	"github.com/Jszigeti/floe/internal/ui"
)

// runningAppsJXA lists the open apps as "<bundle id>\t<name>" lines. It reads NSWorkspace,
// which needs no Automation permission.
const runningAppsJXA = `ObjC.import("AppKit");
function run() {
  return $.NSWorkspace.sharedWorkspace.runningApplications.js
    .filter(a => a.activationPolicy == $.NSApplicationActivationPolicyRegular)
    .map(a => a.bundleIdentifier.js + "\t" + a.localizedName.js)
    .join("\n");
}`

// replaceTarget is what this Mac continues the backup with: the restic binary, the destination
// and this Mac's hardware id, which the taken-over config records.
type replaceTarget struct {
	bin, dest, machine string
}

// replaceRun is a Replace whose every question was answered, ready to restore and install.
type replaceRun struct {
	to      replaceTarget
	backup  foundBackup
	home    string
	runner  run.Runner // hands the terminal over to a command that needs it
	client  *restic.Client
	block   *ui.Block
	live    bool
	power   string
	state   replace.State
	recipe  string
	list    []inventory.App
	plan    apps.Plan
	install apps.Deps
	skipped []string // App Store apps left for later
}

// runReplace makes this Mac the continuation of b: its files, its apps, its floe settings, its
// history. Every question comes first; once the run starts, failures only go to the report.
// resume is the state of the interrupted run, nil for a new Replace.
func runReplace(ctx context.Context, p prompter, d Deps, to replaceTarget, b foundBackup, resume *replace.State) error {
	r, err := askReplace(ctx, p, d, to, b, resume)
	if err != nil {
		return err
	}
	defer r.install.Admin.Close()
	return r.run(ctx, p)
}

// askReplace asks every question a Replace needs before it writes to this home.
func askReplace(ctx context.Context, p prompter, d Deps, to replaceTarget, b foundBackup, resume *replace.State) (replaceRun, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return replaceRun{}, err
	}
	start := time.Now()
	progress := p.cmd.ErrOrStderr()
	live := liveScreen(progress)
	block := ui.NewBlock(progress, live)
	runner := pausing{Runner: d.Runner, block: block}
	client := &restic.Client{Bin: to.bin, Repo: b.Path, R: runner}
	if err := askBackupPassphrase(ctx, p, runner, client, b.Name); err != nil {
		return replaceRun{}, err
	}
	st, err := pinSnapshot(ctx, client, b, to.dest, resume, home)
	if err != nil {
		return replaceRun{}, err
	}
	block.Done("Found the backup, passphrase checked", time.Since(start))
	start = time.Now()
	if err := waitForFullDiskAccess(p, home); err != nil {
		return replaceRun{}, err
	}
	recipe, err := dumpRecipe(ctx, p, client, st.SnapshotID, st.OldHome, home)
	if err != nil {
		return replaceRun{}, err
	}
	list, err := inventoryOf(ctx, recipe, d.CaskURL, home)
	if err != nil {
		return replaceRun{}, err
	}
	plan, err := apps.PlanFrom(recipe, list)
	if err != nil {
		return replaceRun{}, fmt.Errorf("reading the app recipe: %w", err)
	}
	// A resumed run passed it already, and what it restored since would count twice.
	if !st.Restoring {
		if err := checkFreeSpace(ctx, client, st.SnapshotID, home, appsSize(list)); err != nil {
			return replaceRun{}, err
		}
	}
	power := askForPower(ctx, p, runner)
	skipped := openAppStore(ctx, p, runner, &plan)
	install := apps.Deps{Runner: runner, BrewDirs: apps.BrewDirs, AppsRoot: "/Applications"}
	if plan.NeedsAdmin(apps.BrewDirs) {
		install.Admin = startAdmin(ctx, p, runner)
	}
	// The run takes the session over; until then, any way out of here closes it, a panic too.
	handedOver := false
	defer func() {
		if !handedOver {
			install.Admin.Close()
		}
	}()
	if err := waitForAppsToQuit(ctx, p, runner); err != nil {
		return replaceRun{}, err
	}
	block.Done(ready(len(plan.StoreIDs) > 0, install.Admin != nil), time.Since(start))
	handedOver = true
	return replaceRun{
		to: to, backup: b, home: home, runner: runner, client: client, block: block, live: live, power: power,
		state: st, recipe: recipe, list: list, plan: plan, install: install, skipped: skipped,
	}, nil
}

// run restores the home and installs the apps, then has this Mac take the backup over.
func (r replaceRun) run(ctx context.Context, p prompter) error {
	stopAwake := keepAwake(ctx, r.runner)
	defer stopAwake()

	// restic writes a retagged snapshot under a new id, which the state pins instead. A snapshot
	// from before kinds has no tag: home keeps it a home snapshot once it carries replaced.
	st := r.state
	var err error
	if st.SnapshotID, err = r.client.Tag(ctx, st.SnapshotID, []string{"home", "replaced"}, nil); err != nil {
		return fmt.Errorf("keeping the replaced snapshot for a year: %w", err)
	}
	st.Restoring = true
	if err := st.Save(); err != nil {
		return err
	}
	cfg, err := takeOverConfig(ctx, p, r.client, st.SnapshotID, st.OldHome, r.home)
	if err != nil {
		return err
	}
	oldMachine := cfg.Machine
	cfg.Destination, cfg.Repo, cfg.Machine = r.to.dest, r.backup.Path, r.to.machine
	appsSnap, err := pinnedApps(ctx, r.client, st.AppsSnapshotID)
	if err != nil {
		return err
	}

	p.cmd.Printf("→ Restoring %s into %s while its apps install, this takes a while…\n", r.backup.Name, r.home)
	status := &replace.Status{}
	downloads := apps.BrewDownloads(r.home)
	downloaded := func() int64 { return apps.DownloadedBytes(downloads) }
	stopScreen := newScreen(r.block, status.Progress, downloaded, r.live, r.power, time.Now).follow(ctx)
	res, err := replace.Run(ctx, replace.RunDeps{
		Client: r.client, Runner: r.runner, Snapshot: st.SnapshotID, Apps: appsSnap,
		Rule: replace.RuleInput{
			OldHome: st.OldHome, Present: st.Present, Apps: r.list,
			PhotosICloud: manifest.ReadFlag(r.recipe, manifest.PhotosFlag), DesktopDocumentsICloud: manifest.ReadFlag(r.recipe, manifest.DesktopDocumentsFlag),
		},
		NewHome: r.home, OldMachine: oldMachine, Plan: r.plan, Install: r.install, Status: status,
	})
	stopScreen()
	if err != nil {
		return err
	}
	// The actions menu waits on the user: no password or wake lock may outlive the run. An action
	// that needs an administrator asks for the password itself.
	r.install.Admin.Close()
	stopAwake()
	// The run never reached mas, which may not be on this Mac yet.
	if len(r.skipped) > 0 {
		res.Apps.Commands = append(res.Apps.Commands, "brew install mas && mas install "+strings.Join(r.skipped, " "))
	}
	actions := replace.Actions(res, oldMac(ctx, r.runner, r.recipe, r.list))
	report, err := replace.ReportPath()
	if err == nil {
		err = replace.WriteReport(report, res, actions)
	}
	if err != nil {
		p.cmd.Printf("! The full report could not be saved: %v\n", err)
	}
	if len(res.RestoreErrs) > 0 {
		replace.WriteSummary(p.cmd.OutOrStdout(), res, actions, false)
		return errors.New("the replace is not finished: run floe init again to resume")
	}
	if err := takeOver(ctx, r.runner, cfg); err != nil {
		// What the run left must show even when this Mac could not take the backup over, as a
		// to-do since floe stops here.
		replace.WriteSummary(p.cmd.OutOrStdout(), res, actions, false)
		return err
	}
	p.cmd.Printf("✓ This Mac now continues %s's backup, daily at %s.\n\n", r.backup.Name, formatHour(cfg.Hour))
	replace.WriteSummary(p.cmd.OutOrStdout(), res, actions, true)
	offerActions(ctx, p, r.runner, actions)
	return nil
}

// takeOver makes this Mac continue the backup and ends the replace. The config is saved only
// now, so no scheduled backup runs under a restore still writing this home; the state is
// cleared before the actions, since a restart ends floe.
func takeOver(ctx context.Context, r run.Runner, cfg config.Config) error {
	if err := cfg.Save(); err != nil {
		return err
	}
	execPath, err := os.Executable()
	if err != nil {
		return err
	}
	if err := launchd.Install(ctx, r, execPath, cfg.Hour); err != nil {
		return err
	}
	return replace.ClearState()
}

// askBackupPassphrase asks until a passphrase opens the backup, then stores it in the Keychain.
func askBackupPassphrase(ctx context.Context, p prompter, r run.Runner, client *restic.Client, name string) error {
	// An interrupted Replace stored the passphrase that opened this backup.
	if pass, err := keychain.Get(ctx, r); err == nil && pass != "" {
		client.Pass = pass
		if client.Open(ctx) == nil {
			return nil
		}
	}
	for {
		pass, _ := p.secret(fmt.Sprintf("Passphrase of %s's backup, from your password manager", name))
		if pass = strings.TrimSpace(pass); pass == "" {
			return errors.New("stopped: no passphrase given")
		}
		client.Pass = pass
		err := client.Open(ctx)
		if err == nil {
			return keychain.Set(ctx, r, pass)
		}
		if !errors.Is(err, restic.ErrWrongPassphrase) {
			return fmt.Errorf("opening %s: %w", name, err)
		}
		p.cmd.Println("This passphrase does not open that backup, try again.")
	}
}

// waitForFullDiskAccess holds the restore until macOS lets it write Mail, Messages and Safari
// data. com.apple.TCC exists on every account and is readable only with Full Disk Access.
func waitForFullDiskAccess(p prompter, home string) error {
	tcc := filepath.Join(home, "Library/Application Support/com.apple.TCC")
	for {
		if _, err := os.ReadDir(tcc); !errors.Is(err, fs.ErrPermission) {
			return nil
		}
		p.cmd.Printf("Full Disk Access is off, so macOS would refuse to restore Mail, Messages and Safari. Grant it to %s in System Settings → Privacy & Security. If macOS asks to quit and reopen it, do so and run floe init again.\n", hostAppHint())
		if !p.confirm("Press Enter to check again, or q to stop") {
			return errors.New("stopped before restoring: grant Full Disk Access, then run floe init again")
		}
	}
}

// checkFreeSpace requires room for the restored files plus the repository, which iCloud
// downloads while restic reads it, plus the apps to install.
func checkFreeSpace(ctx context.Context, client *restic.Client, id, home string, appBytes int64) error {
	files, err := client.RestoreSize(ctx, id)
	if err != nil {
		return err
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(home, &st); err != nil {
		return err
	}
	free := int64(st.Bavail) * int64(st.Bsize)
	// The repository only fills this disk when it lives on it, as iCloud Drive does: an external
	// disk or a shared folder holds its own copy.
	var repo int64
	if sameVolume(client.Repo, st) {
		if repo, err = client.RepoSize(ctx); err != nil {
			return err
		}
	}
	if free < files+repo+appBytes {
		return fmt.Errorf("not enough free space: %.1f GB of files plus %.1f GB of backup to download plus %.1f GB of apps, %.1f GB free",
			float64(files)/1e9, float64(repo)/1e9, float64(appBytes)/1e9, float64(free)/1e9)
	}
	return nil
}

// sameVolume reports whether path is on the filesystem home's stat describes.
func sameVolume(path string, home syscall.Statfs_t) bool {
	var st syscall.Statfs_t
	return syscall.Statfs(path, &st) == nil && st.Fsid == home.Fsid
}

// waitForAppsToQuit holds the restore while an open app could write over the data restored under it.
func waitForAppsToQuit(ctx context.Context, p prompter, r run.Runner) error {
	for {
		out, err := r.Run(ctx, run.Cmd{Name: "osascript", Args: []string{"-l", "JavaScript", "-e", runningAppsJXA}})
		if err != nil {
			return fmt.Errorf("listing open apps: %w", err)
		}
		open := appsToQuit(out, os.Getenv("__CFBundleIdentifier"))
		if len(open) == 0 {
			return nil
		}
		p.cmd.Printf("Quit these apps first, the restore rewrites their data: %s\n", strings.Join(open, ", "))
		if !p.confirm("Press Enter once they are quit, or q to stop") {
			return errors.New("stopped before restoring: quit your open apps, then run floe init again")
		}
	}
}

// appsToQuit keeps the open apps whose data the restore rewrites: neither Apple's, whose data
// stays this Mac's, nor the terminal floe runs in, whose bundle id macOS gives every process it launches.
func appsToQuit(list, terminal string) []string {
	var names []string
	for line := range strings.Lines(list) {
		id, name, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if ok && id != terminal && !strings.HasPrefix(id, "com.apple.") {
			names = append(names, name)
		}
	}
	return names
}

// dumpRecipe copies the old Mac's app recipe out of the snapshot into ~/.local/state/floe/recipe,
// which outlives the restart that the commands printed for later depend on.
func dumpRecipe(ctx context.Context, p prompter, client *restic.Client, id, oldHome, home string) (string, error) {
	dir := filepath.Join(home, config.StateDir, "recipe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for _, name := range manifest.RecipeFiles {
		data, err := client.Dump(ctx, id, filepath.Join(oldHome, config.StateDir, "manifest", name))
		if err != nil && !errors.Is(err, restic.ErrNotInSnapshot) {
			return "", fmt.Errorf("reading %s from the backup: %w", name, err)
		}
		if err != nil {
			p.cmd.Printf("  %s is not in the backup, skipped\n", name)
			_ = os.Remove(filepath.Join(dir, name)) // an earlier run's copy would stand in for it
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// takeOverConfig is the old Mac's config moved to this home, or the defaults when the snapshot holds none.
func takeOverConfig(ctx context.Context, p prompter, client *restic.Client, id, oldHome, home string) (config.Config, error) {
	data, err := client.Dump(ctx, id, filepath.Join(oldHome, ".config/floe/config.toml"))
	if errors.Is(err, restic.ErrNotInSnapshot) {
		p.cmd.Println("  The backup holds no floe config, using the defaults")
		return config.Default()
	}
	if err != nil {
		return config.Config{}, fmt.Errorf("reading the old Mac's floe config from the backup: %w", err)
	}
	old, err := config.Parse(data)
	if err != nil {
		return config.Config{}, fmt.Errorf("reading the old Mac's floe config: %w", err)
	}
	return old.Rehome(oldHome, home), nil
}

// ready is the start steps' line, naming the App Store and the admin password when they were asked.
func ready(store, admin bool) string {
	switch {
	case store && admin:
		return "Disk access, App Store and admin password ready"
	case store:
		return "Disk access and App Store ready"
	case admin:
		return "Disk access and admin password ready"
	}
	return "Disk access ready"
}

// startAdmin asks for the administrator password, three tries at most, so Homebrew and mas never
// prompt mid-run. nil when the user gives none, sudo refuses it or the account is no administrator:
// the run goes on without.
func startAdmin(ctx context.Context, p prompter, r run.Runner) *apps.Admin {
	for try := range 3 {
		if try > 0 {
			p.cmd.Println("This password was refused, try again.")
		}
		password, ok := p.secret("Administrator password (to install your apps)")
		if !ok || password == "" {
			break
		}
		admin, err := apps.StartAdmin(ctx, r, password)
		if err == nil {
			return admin
		}
		if errors.Is(err, apps.ErrNotAdmin) {
			p.cmd.Println("! This account is not an administrator: the apps that need it may ask for an administrator later.")
			return nil
		}
		if !errors.Is(err, apps.ErrRefused) {
			p.cmd.Printf("! Cannot hand the password to the installers: %v\n", err)
			break
		}
	}
	p.cmd.Println("! Going on without it: the apps that need it may ask for your password later.")
	return nil
}
