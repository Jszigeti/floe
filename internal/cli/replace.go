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

	"github.com/Jszigeti/floe/internal/apps"
	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/keychain"
	"github.com/Jszigeti/floe/internal/launchd"
	"github.com/Jszigeti/floe/internal/restic"
	"github.com/Jszigeti/floe/internal/run"
)

// replaceExcludes keep the old Mac's floe from overwriting the one running the restore, and
// leave the iCloud Drive client's state to iCloud, which rebuilds it.
var replaceExcludes = []string{
	"/.local/bin/floe",
	"/.local/share/floe",
	"/.local/state/floe",
	"/.config/floe",
	"/Library/LaunchAgents/com.floe.backup.plist",
	"/Library/Application Support/CloudDocs",
}

// recipeFiles are the manifest files a backup carries (see internal/manifest).
var recipeFiles = []string{"Brewfile", "mas.txt", "applications.txt"}

// runningAppsJXA lists the open apps but Finder and the one whose bundle id it is given, the
// terminal floe runs in. It reads NSWorkspace, which needs no Automation permission.
const runningAppsJXA = `ObjC.import("AppKit");
function run(argv) {
  return $.NSWorkspace.sharedWorkspace.runningApplications.js
    .filter(a => a.activationPolicy == $.NSApplicationActivationPolicyRegular)
    .filter(a => a.bundleIdentifier.js != argv[0] && a.localizedName.js != "Finder")
    .map(a => a.localizedName.js)
    .join("\n");
}`

// runReplace makes this Mac the continuation of b: its files, its floe settings, its history.
func runReplace(ctx context.Context, p prompter, d Deps, bin, dest, mac string, b foundBackup) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	client := &restic.Client{Bin: bin, Repo: b.Path, R: d.Runner, Progress: p.cmd.OutOrStdout()}
	if err := askBackupPassphrase(ctx, p, d.Runner, client, b.Name); err != nil {
		return err
	}
	if err := waitForFullDiskAccess(p, home); err != nil {
		return err
	}
	snap, err := client.Latest(ctx, "home")
	if err != nil {
		return err
	}
	if len(snap.Paths) != 1 {
		return fmt.Errorf("snapshot %s covers %d folders, not one home folder: restore it with floe restore", snap.ShortID, len(snap.Paths))
	}
	oldHome := snap.Paths[0]
	if err := checkFreeSpace(ctx, client, snap.ShortID, home); err != nil {
		return err
	}
	if err := waitForAppsToQuit(ctx, p, d.Runner); err != nil {
		return err
	}

	p.cmd.Printf("→ Restoring %s into %s, this takes a while…\n", b.Name, home)
	excludeFile, err := os.CreateTemp("", "floe-excludes-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(excludeFile.Name()) }()
	if _, err := excludeFile.WriteString(strings.Join(replaceExcludes, "\n") + "\n"); err != nil {
		return err
	}
	if err := excludeFile.Close(); err != nil {
		return err
	}
	var partial *restic.PartialRestoreError
	if err := client.RestoreFolder(ctx, snap.ShortID, oldHome, home, excludeFile.Name(), func(restic.RestoreStatus) {}); err != nil && !errors.As(err, &partial) {
		return fmt.Errorf("restoring %s: %w", b.Name, err)
	}
	recipe, err := dumpRecipe(ctx, p, client, snap.ShortID, oldHome, home)
	if err != nil {
		return err
	}
	cfg, err := takeOverConfig(ctx, p, client, snap.ShortID, oldHome, home)
	if err != nil {
		return err
	}
	cfg.Destination, cfg.Repo, cfg.Machine = dest, b.Path, mac
	if err := cfg.Save(); err != nil {
		return err
	}
	execPath, err := os.Executable()
	if err != nil {
		return err
	}
	if err := launchd.Install(ctx, d.Runner, execPath, cfg.Hour); err != nil {
		return err
	}

	rep, err := reinstallApps(ctx, p, d.Runner, recipe)
	if err != nil {
		return err
	}
	printAppsReport(p, rep)
	if partial != nil {
		p.cmd.Printf("! %d files could not be restored:\n", len(partial.Failed))
		for _, f := range partial.Failed {
			p.cmd.Printf("    %s\n", filepath.Join(home, f))
		}
	}
	p.cmd.Printf("✓ This Mac now continues %s's backup, daily at %s.\n", b.Name, formatHour(cfg.Hour))
	p.cmd.Println("Restart this Mac now, so macOS and your apps reload the restored data and keychain. Then run `floe doctor`.")
	return nil
}

// askBackupPassphrase asks until a passphrase opens the backup, then stores it in the Keychain.
func askBackupPassphrase(ctx context.Context, p prompter, r run.Runner, client *restic.Client, name string) error {
	for {
		pass := p.ask(fmt.Sprintf("Passphrase of %s's backup, from your password manager", name), "")
		if pass == "" {
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
// downloads while restic reads it.
func checkFreeSpace(ctx context.Context, client *restic.Client, id, home string) error {
	files, err := client.RestoreSize(ctx, id)
	if err != nil {
		return err
	}
	repo, err := client.RepoSize(ctx)
	if err != nil {
		return err
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(home, &st); err != nil {
		return err
	}
	free := int64(st.Bavail) * int64(st.Bsize)
	if free < files+repo {
		return fmt.Errorf("not enough free space: %.1f GB of files plus %.1f GB of backup to download, %.1f GB free",
			float64(files)/1e9, float64(repo)/1e9, float64(free)/1e9)
	}
	return nil
}

// waitForAppsToQuit holds the restore while an open app could write over the data restored under it.
func waitForAppsToQuit(ctx context.Context, p prompter, r run.Runner) error {
	for {
		// macOS gives every process an app launches that app's bundle id, here the terminal's.
		terminal := os.Getenv("__CFBundleIdentifier")
		out, err := r.Run(ctx, run.Cmd{Name: "osascript", Args: []string{"-l", "JavaScript", "-e", runningAppsJXA, terminal}})
		if err != nil {
			return fmt.Errorf("listing open apps: %w", err)
		}
		open := strings.FieldsFunc(out, func(c rune) bool { return c == '\n' })
		if len(open) == 0 {
			return nil
		}
		p.cmd.Printf("Quit these apps first, the restore rewrites their data: %s\n", strings.Join(open, ", "))
		if !p.confirm("Press Enter once they are quit, or q to stop") {
			return errors.New("stopped before restoring: quit your open apps, then run floe init again")
		}
	}
}

// dumpRecipe copies the old Mac's app recipe out of the snapshot into ~/.local/state/floe/recipe,
// which outlives the restart that the commands printed for later depend on.
func dumpRecipe(ctx context.Context, p prompter, client *restic.Client, id, oldHome, home string) (string, error) {
	dir := filepath.Join(home, ".local/state/floe/recipe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for _, name := range recipeFiles {
		data, err := client.Dump(ctx, id, filepath.Join(oldHome, ".local/state/floe/manifest", name))
		if err != nil {
			p.cmd.Printf("  %s is not in the backup, skipped\n", name)
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
	if err != nil {
		p.cmd.Println("  The backup holds no floe config, using the defaults")
		return config.Default()
	}
	old, err := config.Parse(data)
	if err != nil {
		return config.Config{}, fmt.Errorf("reading the old Mac's floe config: %w", err)
	}
	return old.Rehome(oldHome, home), nil
}

// reinstallApps installs the recipe's apps under one administrator password, naming each as it starts.
func reinstallApps(ctx context.Context, p prompter, r run.Runner, recipe string) (apps.Report, error) {
	plan, err := apps.PlanFrom(recipe, apps.ListedApps(recipe, nil))
	if err != nil {
		return apps.Report{}, fmt.Errorf("reading the app recipe: %w", err)
	}
	deps := apps.Deps{Runner: r, BrewDirs: apps.BrewDirs, AppsRoot: "/Applications"}
	if plan.Brewfile != "" || plan.VSCode != "" || len(plan.Casks) > 0 || len(plan.StoreIDs) > 0 {
		deps.Admin = startAdmin(ctx, p, r)
		defer deps.Admin.Close()
		p.cmd.Println("→ Installing your apps…")
	}
	current := ""
	return apps.Install(ctx, deps, plan, func(pr apps.Progress) {
		if pr.Current != "" && pr.Current != current {
			current = pr.Current
			p.cmd.Printf("  %d of %d  %s\n", pr.Done, pr.Total, current)
		}
	}), nil
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

// printAppsReport lists what the apps step left to do by hand.
func printAppsReport(p prompter, rep apps.Report) {
	if len(rep.BrewFailed) > 0 {
		p.cmd.Printf("! Homebrew could not install: %s\n", strings.Join(rep.BrewFailed, ", "))
	}
	for _, n := range rep.Notes {
		p.cmd.Printf("! %s\n", n)
	}
	if len(rep.Missing) > 0 {
		p.cmd.Printf("! Reinstall these apps by hand: %s\n", strings.Join(rep.Missing, ", "))
	}
	for _, c := range rep.Commands {
		if len(rep.StoreFailed) > 0 && c == "mas install "+strings.Join(rep.StoreFailed, " ") {
			p.cmd.Println("! Some App Store apps did not install. If the App Store is signed out, sign in to it, then run the command below.")
		}
		p.cmd.Printf("! Run later: %s\n", c)
	}
}
