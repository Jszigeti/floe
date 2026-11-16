package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/askpass"
	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/launchd"
	"github.com/Jszigeti/floe/internal/replace"
	"github.com/Jszigeti/floe/internal/run"
)

const oldMacConfig = `destination = "/Users/alice/Library/Mobile Documents/com~apple~CloudDocs"
hour = 9

[retention]
daily = 3
weekly = 2
monthly = 1

[scope]
paths = ["/Users/alice"]
exclude = ["/Users/alice/Library/Caches", "node_modules"]
`

const aliceSnapshot = `[{"time":"2026-09-18T13:00:00Z","short_id":"bbb222","paths":["/Users/alice"]}]`

// replaceFake answers every command a Replace runs on thisMac, with restore failing as given.
func replaceFake(restoreErr error, totalSize string) *run.Fake {
	return &run.Fake{Handler: func(c run.Cmd) (string, error) {
		arg := func(i int) string {
			if len(c.Args) > i {
				return c.Args[i]
			}
			return ""
		}
		switch {
		case c.Name == "ioreg":
			return ioregOut, nil
		case c.Name == "security" && arg(0) == "find-generic-password":
			return "", errKeychainNotFound
		case arg(0) == "snapshots":
			return aliceSnapshot, nil
		case arg(0) == "stats":
			return `{"total_size":` + totalSize + `}`, nil
		case arg(0) == "restore":
			return "", restoreErr
		case arg(0) == "dump" && strings.HasSuffix(arg(2), "/config.toml"):
			return oldMacConfig, nil
		case arg(0) == "dump":
			return "", errors.New(`Fatal: cannot dump file: path not found in snapshot`)
		}
		return "", nil
	}}
}

// startReplace sets up a new Mac's home whose destination holds an old Mac's backup saved ten days ago.
func startReplace(t *testing.T) (home, dest, repo string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	home = os.Getenv("HOME")
	installFakeRestic(t)
	dest = filepath.Join(home, "dest")
	repo = fakeRepo(t, dest, "MacBook-Air", 10*24*time.Hour)
	return home, dest, repo
}

// resticCall is the first restic call running sub, nil when none did.
func resticCall(f *run.Fake, sub string) *run.Cmd {
	i := slices.IndexFunc(f.Calls, func(c run.Cmd) bool {
		return strings.HasSuffix(c.Name, "/restic") && len(c.Args) > 0 && c.Args[0] == sub
	})
	if i < 0 {
		return nil
	}
	return &f.Calls[i]
}

func TestReplaceRestoresTheOldHomeAndTakesOverItsConfig(t *testing.T) {
	home, dest, repo := startReplace(t)
	f := replaceFake(nil, "1000")
	base := f.Handler
	var excludes string
	f.Handler = func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "restore" {
			data, err := os.ReadFile(c.Args[len(c.Args)-1])
			excludes = string(data)
			if err != nil {
				return "", err
			}
		}
		return base(c)
	}
	out, err := runInit(f, dest+"\n\npw\n")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	for _, want := range []string{"Found a backup: MacBook-Air, last saved ", "Choice [1]: ", "  R  Restart now\n",
		"✓ Found the backup, passphrase checked", "\nRestoring files  loading the backup…\n", "\nInstalling apps\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Fatalf("off a terminal the replace prints no escape sequence:\n%q", out)
	}
	c := resticCall(f, "restore")
	if c == nil || !slices.Equal(c.Args[:len(c.Args)-1], []string{"restore", "--json", "bbb222:/Users/alice", "--target", home, "--exclude-file"}) {
		t.Fatalf("restore call = %+v", c)
	}
	wantExcludes := "/.Trash\n/Library/*\n/.local/bin/floe\n/.local/share/floe\n/.local/state/floe\n/.config/floe\n" +
		"/Library/LaunchAgents/com.floe.backup.plist\n/Library/Logs/floe.log\n/Library/Preferences\n"
	if excludes != wantExcludes {
		t.Fatalf("exclude file = %q", excludes)
	}
	if fileExists(c.Args[len(c.Args)-1]) {
		t.Fatal("exclude file left behind")
	}
	if !f.CalledWith("security", "add-generic-password", "-w pw") {
		t.Fatal("the pasted passphrase is not stored in the Keychain")
	}
	if resticCall(f, "backup") != nil || resticCall(f, "init") != nil {
		t.Fatal("a Replace neither initializes nor backs up")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Repo != repo || cfg.Machine != thisMac || cfg.Destination != dest || cfg.Hour != 9 || cfg.Retention.Daily != 3 {
		t.Fatalf("config = %+v", cfg)
	}
	if !slices.Equal(cfg.Scope.Paths, []string{home}) || !slices.Contains(cfg.Scope.Exclude, filepath.Join(home, "Library/Caches")) {
		t.Fatalf("scope not moved to %s: %+v", home, cfg.Scope)
	}
	if pp, _ := launchd.PlistPath(); !fileExists(pp) {
		t.Fatal("schedule not installed")
	}
}

func TestReplaceUsesTheDefaultsWithoutAnOldConfig(t *testing.T) {
	_, dest, _ := startReplace(t)
	f := replaceFake(nil, "1000")
	base := f.Handler
	f.Handler = func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "dump" {
			return "", errors.New("not found in snapshot")
		}
		return base(c)
	}
	out, err := runInit(f, dest+"\n\npw\n")
	if err != nil || !strings.Contains(out, "no floe config") {
		t.Fatalf("init: %v\n%s", err, out)
	}
	cfg, err := config.Load()
	if err != nil || cfg.Hour != 13 {
		t.Fatalf("config = %+v, %v", cfg, err)
	}
}

func TestReplaceAsksAgainOnAWrongPassphrase(t *testing.T) {
	_, dest, _ := startReplace(t)
	f := replaceFake(nil, "1000")
	base := f.Handler
	wrong := capturedExitErr(12)
	f.Handler = func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "cat" && slices.Contains(c.Env, "RESTIC_PASSWORD=wrong") {
			return "", wrong
		}
		return base(c)
	}
	out, err := runInit(f, dest+"\n\nwrong\npw\n")
	if err != nil || !strings.Contains(out, "does not open that backup") {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if f.CalledWith("add-generic-password", "-w wrong") {
		t.Fatal("a wrong passphrase reached the Keychain")
	}
}

func TestReplaceStopsWithoutFullDiskAccess(t *testing.T) {
	home, dest, _ := startReplace(t)
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	tcc := filepath.Join(home, "Library/Application Support/com.apple.TCC")
	if err := os.MkdirAll(tcc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tcc, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tcc, 0o755) })
	f := replaceFake(nil, "1000")
	out, err := runInit(f, dest+"\n\npw\n")
	if err == nil || !strings.Contains(err.Error(), "Full Disk Access") || !strings.Contains(out, "Grant it to Terminal") {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if resticCall(f, "restore") != nil {
		t.Fatal("restored without Full Disk Access")
	}
}

func TestReplaceStopsWhenTheDiskIsTooSmall(t *testing.T) {
	_, dest, _ := startReplace(t)
	f := replaceFake(nil, "1152921504606846976") // 1 EiB of files and as much of backup
	_, err := runInit(f, dest+"\n\npw\n")
	if err == nil || !strings.Contains(err.Error(), "not enough free space") {
		t.Fatalf("err = %v", err)
	}
	if resticCall(f, "restore") != nil {
		t.Fatal("restored without room for it")
	}
}

func TestReplaceWaitsForOpenApps(t *testing.T) {
	_, dest, _ := startReplace(t)
	t.Setenv("__CFBundleIdentifier", "com.googlecode.iterm2")
	f := replaceFake(nil, "1000")
	base := f.Handler
	listed := 0
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "osascript" && slices.Contains(c.Args, "JavaScript") {
			listed++
			if listed == 1 {
				return "com.googlecode.iterm2\tiTerm2\ncom.apple.AppStore\tApp Store\ncom.tinyspeck.slackmacgap\tSlack\ncom.spotify.client\tSpotify\n", nil
			}
			return "com.apple.Safari\tSafari\n", nil
		}
		return base(c)
	}
	out, err := runInit(f, dest+"\n\npw\n\n")
	if err != nil || !strings.Contains(out, "rewrites their data: Slack, Spotify\n") || resticCall(f, "restore") == nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
}

func TestReplaceStopsWhileAppsStayOpen(t *testing.T) {
	_, dest, _ := startReplace(t)
	f := replaceFake(nil, "1000")
	base := f.Handler
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "osascript" && slices.Contains(c.Args, "JavaScript") {
			return "com.tinyspeck.slackmacgap\tSlack\n", nil
		}
		return base(c)
	}
	if _, err := runInit(f, dest+"\n\npw\n"); err == nil || !strings.Contains(err.Error(), "quit your open apps") {
		t.Fatalf("err = %v", err)
	}
	if resticCall(f, "restore") != nil {
		t.Fatal("restored under an open app")
	}
}

func TestReplaceListsFailedFilesAndGoesOn(t *testing.T) {
	home, dest, repo := startReplace(t)
	_, partial := run.ExecRunner{}.Run(context.Background(), run.Cmd{Name: "sh", Args: []string{"-c",
		`printf '{"message_type":"error","error":{"message":"open: operation not permitted"},"during":"restore","item":"/Library/Mail/V10/x.emlx"}\n' >&2; exit 1`}})
	f := replaceFake(partial, "1000")
	out, err := runInit(f, dest+"\n\npw\n")
	if err != nil {
		t.Fatalf("a partial restore must not stop the next steps: %v\n%s", err, out)
	}
	failed := filepath.Join(home, "Library/Mail/V10/x.emlx")
	if !strings.Contains(out, "Check the file floe could not restore, in the full report") || strings.Contains(out, failed) {
		t.Fatalf("want the failed file counted on screen, its path in the report only:\n%s", out)
	}
	if report, err := os.ReadFile(filepath.Join(home, ".local/state/floe/replace-report.txt")); err != nil || !strings.Contains(string(report), failed) {
		t.Fatalf("failed path not in the report: %q, %v", report, err)
	}
	if cfg, err := config.Load(); err != nil || cfg.Repo != repo || !f.CalledWith("launchctl", "bootstrap") {
		t.Fatalf("config and schedule must follow a partial restore: %+v, %v", cfg, err)
	}
}

func TestReplaceStopsAskingWhenTheAccountIsNotAnAdmin(t *testing.T) {
	f, dest, calls := appsReplace(t, "s3cret")
	base := f.Handler
	f.Handler = func(c run.Cmd) (string, error) {
		if filepath.Base(c.Name) == "sudo" && slices.Contains(c.Args, "-A") {
			return "", errors.New("sudo [-A -v]: exit status 1: jonas is not in the sudoers file.")
		}
		return base(c)
	}
	out, err := runInit(f, dest+"\n\npw\ns3cret\n")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	got := calls()
	if len(got) != 4 || got[0] != "sudo -k " || !strings.HasPrefix(got[1], "sudo -A ") || got[3] != "brew bundle " {
		t.Fatalf("calls = %q, want the old timestamp dropped, one check, then brew without the password", got)
	}
	if strings.Contains(out, "refused") || !strings.Contains(out, "! This account is not an administrator: the apps that need it may ask for an administrator later.") {
		t.Fatalf("output:\n%s", out)
	}
}

// appsReplace is a Replace whose recipe installs jq and zed with brew, and whose sudo accepts only
// password, read from the socket floe serves. calls lists its admin and brew bundle calls.
func appsReplace(t *testing.T, password string) (*run.Fake, string, func() []string) {
	t.Helper()
	_, dest, _ := startReplace(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "brew"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	f := replaceFake(nil, "1000")
	base := f.Handler
	f.Handler = func(c run.Cmd) (string, error) {
		switch {
		case len(c.Args) > 2 && c.Args[0] == "dump" && strings.HasSuffix(c.Args[2], "/Brewfile"):
			return "brew \"jq\"\ncask \"zed\"\n", nil
		case filepath.Base(c.Name) == "sudo" && slices.Contains(c.Args, "-A"):
			program, _ := strings.CutPrefix(c.Env[0], "SUDO_ASKPASS=")
			var got bytes.Buffer
			if err := askpass.Answer(filepath.Join(filepath.Dir(program), "socket"), &got); err != nil || got.String() != password+"\n" {
				return "", errors.New("Sorry, try again.")
			}
		}
		return base(c)
	}
	return f, dest, func() []string {
		var calls []string
		for _, c := range f.Calls {
			if filepath.Base(c.Name) == "sudo" || isBundle(c) {
				calls = append(calls, filepath.Base(c.Name)+" "+c.Args[0]+" "+strings.Join(c.Env, " "))
			}
		}
		return calls
	}
}

func isBundle(c run.Cmd) bool {
	return filepath.Base(c.Name) == "brew" && len(c.Args) > 0 && c.Args[0] == "bundle"
}

func TestReplaceGivesTheAdminPasswordToTheApps(t *testing.T) {
	f, dest, calls := appsReplace(t, "s3cret")
	out, err := runInit(f, dest+"\n\npw\ns3cret\n")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Administrator password (to install your apps): ") {
		t.Fatalf("no password prompt:\n%s", out)
	}
	got := calls()
	if len(got) != 4 || got[0] != "sudo -k " || !strings.HasPrefix(got[1], "sudo -A SUDO_ASKPASS=") || !strings.HasPrefix(got[2], "brew bundle SUDO_ASKPASS=") || got[3] != "sudo -k " {
		t.Fatalf("calls = %q, want the old timestamp dropped, the password checked, given to brew, then dropped", got)
	}
}

func TestReplaceEndsWithTheActionsLeft(t *testing.T) {
	home, dest, _ := startReplace(t)
	f := replaceFake(nil, "1000")
	withAnswers(f, map[string]string{
		"mas.txt":                  "409183694  Keynote  (15.2)\n",
		"keychain-apps.txt":        "Chrome\nSlack\n",
		"desktop-documents-icloud": "true\n",
		"pmset -g batt":            "Now drawing from 'AC Power'\n",
	})
	// The destination, the backup, its passphrase, the App Store skipped, then the App Store
	// action, a choice floe cannot run, a wrong one, and the end of the input.
	out, err := runInit(f, dest+"\n\npw\ns\n3\n2\n9\nr\n")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	for _, want := range []string{
		"✓ This Mac now continues MacBook-Air's backup",
		"✓ Restored 0.0 GB of files and settings",
		"  1  Sign in again to Chrome and Slack\n",
		"  2  Turn on Desktop & Documents in iCloud Drive\n",
		"  3  Sign in to the App Store, then install Keynote   type 3, floe does it\n",
		"  R  Restart now\n",
		"Full report: ~/.local/state/floe/replace-report.txt\n",
		"Only you can do this one.",
		"Type one of the numbers above, R, or Enter to quit.",
		"Choice, or Enter to quit: Restarting…\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	// The App Store was skipped, so nothing installed mas yet.
	if !f.CalledWith("/bin/sh -c brew install mas && mas install 409183694") {
		t.Fatalf("the chosen action did not run; calls = %+v", f.Calls)
	}
	report, err := os.ReadFile(filepath.Join(home, ".local/state/floe/replace-report.txt"))
	if err != nil || !strings.Contains(string(report), "mas install 409183694") {
		t.Fatalf("report = %q, %v", report, err)
	}
	if st, ok, _ := replace.LoadState(); ok {
		t.Fatalf("state = %+v, want it cleared before the actions, a restart may end floe", st)
	}
}

// orderRunner logs, in one list with what floe prints, each command and when caffeinate stops.
// Its caffeinate runs until its context ends, as the real one does.
type orderRunner struct {
	*run.Fake
	mu  sync.Mutex
	log []string
}

func (o *orderRunner) add(entry string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.log = append(o.log, entry)
}

func (o *orderRunner) Run(ctx context.Context, c run.Cmd) (string, error) {
	if c.Name == "caffeinate" {
		<-ctx.Done()
		o.add("caffeinate stopped")
		return "", ctx.Err()
	}
	o.add(filepath.Base(c.Name) + " " + strings.Join(c.Args, " "))
	return o.Fake.Run(ctx, c)
}

func (o *orderRunner) Write(p []byte) (int, error) {
	o.add("out " + string(p))
	return len(p), nil
}

func TestReplaceEndsTheAdminRightsAndTheWakeBeforeTheSummary(t *testing.T) {
	f, dest, _ := appsReplace(t, "s3cret")
	o := &orderRunner{Fake: f}
	cmd := Root(Deps{Version: "t", Runner: o})
	cmd.SetOut(o)
	cmd.SetIn(strings.NewReader(dest + "\n\npw\ns3cret\n"))
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init: %v\n%q", err, o.log)
	}
	at := func(entry string) int {
		return slices.IndexFunc(o.log, func(e string) bool { return strings.HasPrefix(e, entry) })
	}
	var drops []int // sudo -k drops the timestamp before the password is asked, then ends the rights
	for i, e := range o.log {
		if e == "sudo -k" {
			drops = append(drops, i)
		}
	}
	summary := at("out ✓ Restored")
	if summary < 0 || at("caffeinate stopped") > summary || len(drops) != 2 || drops[1] > summary {
		t.Fatalf("the menu waits for the user: the password and the wake must end before it\n%q", o.log)
	}
}

func TestReplaceGoesOnWithoutAdminAfterThreeRefusals(t *testing.T) {
	f, dest, calls := appsReplace(t, "s3cret")
	out, err := runInit(f, dest+"\n\npw\na\nb\nc\n")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	got := calls()
	if len(got) != 10 || got[9] != "brew bundle " {
		t.Fatalf("calls = %q, want three refusals then brew without the password", got)
	}
	if strings.Count(out, "This password was refused") != 2 || !strings.Contains(out, "may ask for your password later") {
		t.Fatalf("output:\n%s", out)
	}
}

// withAnswers makes f answer the commands whose name and args start with a key, and the dumps
// of manifest files named by a key ending in their file name, before its usual answers.
func withAnswers(f *run.Fake, answers map[string]string) {
	base := f.Handler
	f.Handler = func(c run.Cmd) (string, error) {
		line := c.Name + " " + strings.Join(c.Args, " ")
		for key, out := range answers {
			if strings.HasPrefix(line, key) || len(c.Args) > 2 && c.Args[0] == "dump" && strings.HasSuffix(c.Args[2], "/"+key) {
				return out, nil
			}
		}
		return base(c)
	}
}

// brewOnPath puts a brew that does nothing first on PATH, so the apps task finds Homebrew installed.
func brewOnPath(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "brew"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func TestReplaceAsksOnlyWhatIsNeeded(t *testing.T) {
	_, dest, _ := startReplace(t)
	brewOnPath(t)
	f := replaceFake(nil, "1000")
	withAnswers(f, map[string]string{"pmset -g batt": "Now drawing from 'AC Power'\n", "Brewfile": "brew \"jq\"\n"})
	out, err := runInit(f, dest+"\n\npw\n")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	for _, never := range []string{"battery", "App Store", "Administrator password", "Full Disk Access"} {
		if strings.Contains(out, never) {
			t.Errorf("asked about %q with nothing needing it:\n%s", never, out)
		}
	}
	if f.CalledWith("open", "-a") || !f.CalledWith("bundle", "install") {
		t.Fatalf("calls = %+v", f.Calls)
	}
}

func TestReplaceAsksForPowerAndCanSkipTheAppStore(t *testing.T) {
	_, dest, _ := startReplace(t)
	f := replaceFake(nil, "1000")
	withAnswers(f, map[string]string{
		"pmset -g batt": "Now drawing from 'Battery Power'\n -InternalBattery-0 (id=1)	80%; discharging\n",
		"mas.txt":       "409183694  Keynote  (15.2)\n",
	})
	out, err := runInit(f, dest+"\n\npw\n\ns\n")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if !strings.Contains(out, "plug it in and keep its lid open") || !f.CalledWith("open -a App Store") {
		t.Fatalf("power or App Store step missing:\n%s", out)
	}
	if strings.Contains(out, "Administrator password") || !strings.Contains(out, "Sign in to the App Store, then install Keynote") {
		t.Fatalf("a skipped App Store must leave its apps for later, with nothing else to install:\n%s", out)
	}
}

func TestReplaceAsksTheAdminPasswordForAppStoreApps(t *testing.T) {
	_, dest, _ := startReplace(t)
	f := replaceFake(nil, "1000")
	withAnswers(f, map[string]string{"mas.txt": "409183694  Keynote  (15.2)\n"})
	out, err := runInit(f, dest+"\n\npw\n\n")
	if err != nil || !strings.Contains(out, "Administrator password (to install your apps): ") {
		t.Fatalf("init: %v\n%s", err, out)
	}
}

func TestReplaceTagsTheReplacedSnapshot(t *testing.T) {
	_, dest, _ := startReplace(t)
	f := replaceFake(nil, "1000")
	withAnswers(f, map[string]string{
		filepath.Join(os.Getenv("HOME"), ".local/share/floe/bin/restic") + " tag": `{"message_type":"changed","old_snapshot_id":"bbb222ff","new_snapshot_id":"ddd444ff"}` + "\n",
	})
	out, err := runInit(f, dest+"\n\npw\n")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	tag := resticCall(f, "tag")
	if tag == nil || !slices.Equal(tag.Args, []string{"tag", "--json", "--add", "home", "--add", "replaced", "bbb222"}) {
		t.Fatalf("tag call = %+v", tag)
	}
	if c := resticCall(f, "restore"); c == nil || c.Args[2] != "ddd444ff:/Users/alice" {
		t.Fatalf("restore call = %+v, want the retagged snapshot's new id", c)
	}
}

func TestReplaceInstallsTheInventoryCasks(t *testing.T) {
	_, dest, _ := startReplace(t)
	brewOnPath(t)
	f := replaceFake(nil, "1000")
	withAnswers(f, map[string]string{"apps.json": `[{"name":"Google Chrome","path":"/Applications/Google Chrome.app","source":"cask","cask":"google-chrome","size":1}]`})
	out, err := runInit(f, dest+"\n\npw\n")
	if err != nil || !f.CalledWith("brew install --cask --adopt google-chrome") {
		t.Fatalf("init: %v, calls = %+v\n%s", err, f.Calls, out)
	}
}

func TestReplaceMatchesListedAppsWithoutAnInventory(t *testing.T) {
	home, dest, _ := startReplace(t)
	brewOnPath(t)
	cache := filepath.Join(home, ".local/state/floe/cask.json")
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, []byte(`[{"token":"google-chrome","artifacts":[{"app":["Google Chrome.app"]}]}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	f := replaceFake(nil, "1000")
	withAnswers(f, map[string]string{"applications.txt": "Google Chrome.app\nSafari.app\n"})
	out, err := runInit(f, dest+"\n\npw\n")
	if err != nil || !f.CalledWith("brew install --cask --adopt google-chrome") {
		t.Fatalf("init: %v, calls = %+v\n%s", err, f.Calls, out)
	}
}

func TestReplaceCountsTheAppsInTheFreeSpace(t *testing.T) {
	_, dest, _ := startReplace(t)
	f := replaceFake(nil, "1000")
	withAnswers(f, map[string]string{"apps.json": `[{"name":"Huge","path":"/Applications/Huge.app","source":"backup","size":1152921504606846976}]`})
	if _, err := runInit(f, dest+"\n\npw\n"); err == nil || !strings.Contains(err.Error(), "GB of apps") {
		t.Fatalf("err = %v", err)
	}
}

func TestReplaceKeepsTheMacAwake(t *testing.T) {
	_, dest, _ := startReplace(t)
	f := replaceFake(nil, "1000")
	if out, err := runInit(f, dest+"\n\npw\n"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if !f.CalledWith("caffeinate -i -w " + strconv.Itoa(os.Getpid())) {
		t.Fatalf("calls = %+v", f.Calls)
	}
}

func TestReplaceStopsUnfinishedWhenARestoreFails(t *testing.T) {
	_, dest, _ := startReplace(t)
	f := replaceFake(errors.New("Fatal: unable to open repository"), "1000")
	out, err := runInit(f, dest+"\n\npw\n")
	if err == nil || err.Error() != "the replace is not finished: run floe init again to resume" {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if !strings.Contains(out, "! Part of the backup was not restored:\n  restoring /Users/alice: ") ||
		strings.Contains(out, "now continues") || strings.Contains(out, "✓ Restored") || strings.Contains(out, "Restart now") {
		t.Fatalf("want the failed step listed and no success:\n%s", out)
	}
	if st, ok, _ := replace.LoadState(); !ok || !st.Restoring {
		t.Fatalf("state = %+v, %v, want it kept for the resume", st, ok)
	}
	if cp, _ := config.Path(); fileExists(cp) {
		t.Fatal("config saved by an unfinished replace")
	}
	if pp, _ := launchd.PlistPath(); fileExists(pp) || f.CalledWith("launchctl", "bootstrap") {
		t.Fatal("schedule installed by an unfinished replace")
	}
}

func TestReplaceShowsTheSummaryWhenTheScheduleFails(t *testing.T) {
	_, dest, _ := startReplace(t)
	f := replaceFake(nil, "1000")
	withAnswers(f, map[string]string{"mas.txt": "409183694  Keynote  (15.2)\n"})
	base := f.Handler
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "launchctl" && len(c.Args) > 0 && c.Args[0] == "bootstrap" {
			return "", errors.New("launchctl bootstrap: Input/output error")
		}
		return base(c)
	}
	out, err := runInit(f, dest+"\n\npw\ns\n")
	if err == nil || !strings.Contains(out, "Sign in to the App Store, then install Keynote") || strings.Contains(out, "now continues") {
		t.Fatalf("a failed schedule must still show what is left: %v\n%s", err, out)
	}
	if strings.Contains(out, "floe does it") {
		t.Fatalf("floe offers no action after a failed schedule, the list is a to-do:\n%s", out)
	}
}

func TestReplaceSchedulesBackupsOnlyOnceRestored(t *testing.T) {
	_, dest, _ := startReplace(t)
	cp, _ := config.Path()
	pp, _ := launchd.PlistPath()
	f := replaceFake(nil, "1000")
	base := f.Handler
	early := false
	f.Handler = func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "restore" && (fileExists(cp) || fileExists(pp)) {
			early = true
		}
		return base(c)
	}
	if out, err := runInit(f, dest+"\n\npw\n"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if early {
		t.Fatal("config or schedule in place while the restore runs: a scheduled backup could start mid-restore")
	}
	if !fileExists(cp) || !fileExists(pp) {
		t.Fatal("config or schedule missing after the replace")
	}
}

func TestReplaceWaitsForOpenAppsLast(t *testing.T) {
	_, dest, _ := startReplace(t)
	f := replaceFake(nil, "1000")
	withAnswers(f, map[string]string{"mas.txt": "409183694  Keynote  (15.2)\n"})
	base := f.Handler
	listed := 0
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "osascript" && slices.Contains(c.Args, "JavaScript") {
			listed++
			if listed == 1 {
				return "com.tinyspeck.slackmacgap\tSlack\n", nil
			}
			return "", nil
		}
		return base(c)
	}
	// The destination, the backup, its passphrase, the App Store sign-in, no admin password, Safari quit.
	out, err := runInit(f, dest+"\n\npw\n\n\n\n")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	signIn, admin, quit := strings.Index(out, "sign in to it"), strings.Index(out, "Administrator password"), strings.Index(out, "Quit these apps")
	if signIn < 0 || admin < 0 || quit < signIn || quit < admin {
		t.Fatalf("want the apps to quit asked after the App Store and the admin password:\n%s", out)
	}
}

func TestSameVolumeTellsDisksApart(t *testing.T) {
	var home syscall.Statfs_t
	if err := syscall.Statfs(t.TempDir(), &home); err != nil {
		t.Fatal(err)
	}
	if !sameVolume(t.TempDir(), home) {
		t.Error("two temp folders must be on the same volume")
	}
	if sameVolume("/dev", home) {
		t.Error("/dev is its own filesystem, a backup there would not fill the home's disk")
	}
	if sameVolume(filepath.Join(t.TempDir(), "missing"), home) {
		t.Error("a path that does not exist is not counted")
	}
}

func TestOutcomeSaysWhatTheActionLeft(t *testing.T) {
	homebrew := replace.Action{Key: "1", Label: "Install Homebrew", Command: "x"}
	restart := replace.Action{Key: "R", Label: "Restart now", Command: "x"}
	cases := []struct {
		err  error
		a    replace.Action
		want string
	}{
		{replace.ErrNoHomebrew, homebrew, "! Homebrew is still not installed: its download failed, check the internet connection and try again."},
		{errors.New("exit status 1"), homebrew, "! It did not finish, see the lines above."},
		{nil, homebrew, "✓ Done: Install Homebrew"},
		{nil, restart, "Restarting…"},
	}
	for _, c := range cases {
		if got := outcome(c.err, c.a); got != c.want {
			t.Errorf("outcome(%v, %s) = %q, want %q", c.err, c.a.Key, got, c.want)
		}
	}
}
