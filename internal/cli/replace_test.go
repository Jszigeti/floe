package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/apps"
	"github.com/Jszigeti/floe/internal/askpass"
	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/launchd"
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
	for _, want := range []string{"Found a backup: MacBook-Air, last saved ", "Choice [1]: ", "Restart this Mac"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	c := resticCall(f, "restore")
	if c == nil || !slices.Equal(c.Args[:len(c.Args)-1], []string{"restore", "--json", "bbb222:/Users/alice", "--target", home, "--exclude-file"}) {
		t.Fatalf("restore call = %+v", c)
	}
	wantExcludes := "/.local/bin/floe\n/.local/share/floe\n/.local/state/floe\n/.config/floe\n" +
		"/Library/LaunchAgents/com.floe.backup.plist\n/Library/Application Support/CloudDocs\n"
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
	t.Setenv("__CFBundleIdentifier", "com.apple.Terminal")
	f := replaceFake(nil, "1000")
	base := f.Handler
	listed := 0
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "osascript" && slices.Contains(c.Args, "JavaScript") {
			if got := c.Args[len(c.Args)-1]; got != "com.apple.Terminal" {
				t.Errorf("apps listed sparing %q, want the terminal floe runs in", got)
			}
			listed++
			if listed == 1 {
				return "Safari\nMail\n", nil
			}
			return "", nil
		}
		return base(c)
	}
	out, err := runInit(f, dest+"\n\npw\n\n")
	if err != nil || !strings.Contains(out, "Safari, Mail") || resticCall(f, "restore") == nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
}

func TestReplaceStopsWhileAppsStayOpen(t *testing.T) {
	_, dest, _ := startReplace(t)
	f := replaceFake(nil, "1000")
	base := f.Handler
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "osascript" && slices.Contains(c.Args, "JavaScript") {
			return "Safari\n", nil
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
	if !strings.Contains(out, "1 files could not be restored") || !strings.Contains(out, filepath.Join(home, "Library/Mail/V10/x.emlx")) {
		t.Fatalf("failed path not listed:\n%s", out)
	}
	if cfg, err := config.Load(); err != nil || cfg.Repo != repo || !f.CalledWith("launchctl", "bootstrap") {
		t.Fatalf("config and schedule must follow a partial restore: %+v, %v", cfg, err)
	}
}

func TestReplaceReinstallsAppsAfterTheRestore(t *testing.T) {
	f, dest, _ := appsReplace(t, "")
	out, err := runInit(f, dest+"\n\npw\n")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	restoreAt := slices.IndexFunc(f.Calls, func(c run.Cmd) bool { return len(c.Args) > 0 && c.Args[0] == "restore" })
	bundleAt := slices.IndexFunc(f.Calls, isBundle)
	if restoreAt < 0 || bundleAt < restoreAt {
		t.Fatalf("brew bundle must run after the restore: restore at %d, bundle at %d", restoreAt, bundleAt)
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

// appsReplace is a Replace whose recipe installs jq with brew, and whose sudo accepts only
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
			return "brew \"jq\"\n", nil
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

func TestPrintAppsReportSaysWhyAppsAreLeft(t *testing.T) {
	p, out := testPrompter("")
	printAppsReport(p, apps.Report{
		StoreFailed: []string{"111", "222"},
		Notes:       []string{"Homebrew needs an administrator password"},
		Commands:    []string{apps.HomebrewInstall, "mas install 111 222"},
	})
	want := "! Homebrew needs an administrator password\n" +
		"! Run later: " + apps.HomebrewInstall + "\n" +
		"! Some App Store apps did not install. If the App Store is signed out, sign in to it, then run the command below.\n" +
		"! Run later: mas install 111 222\n"
	if out.String() != want {
		t.Fatalf("report:\n%s\nwant:\n%s", out, want)
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
