package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
	out, err := runInit(f, dest+"\n\npw\n")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	for _, want := range []string{"Found a backup: MacBook-Air, last saved ", "Choice [1]: ", "Restart this Mac"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	want := []string{"restore", "bbb222:/Users/alice", "--target", home,
		"--exclude", "/.local/bin/floe",
		"--exclude", "/.local/share/floe",
		"--exclude", "/.local/state/floe",
		"--exclude", "/.config/floe",
		"--exclude", "/Library/LaunchAgents/com.floe.backup.plist",
		"--exclude", "/Library/Application Support/CloudDocs"}
	if c := resticCall(f, "restore"); c == nil || !slices.Equal(c.Args, want) {
		t.Fatalf("restore call = %+v", c)
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
		`printf 'ignoring error for /Library/Mail/V10/x.emlx: operation not permitted\n' >&2; exit 1`}})
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
	home, dest, _ := startReplace(t)
	bin := t.TempDir()
	brew := filepath.Join(bin, "brew")
	if err := os.WriteFile(brew, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	f := replaceFake(nil, "1000")
	base := f.Handler
	f.Handler = func(c run.Cmd) (string, error) {
		if len(c.Args) > 2 && c.Args[0] == "dump" && strings.HasSuffix(c.Args[2], "/Brewfile") {
			return "brew \"jq\"\n", nil
		}
		return base(c)
	}
	out, err := runInit(f, dest+"\n\npw\n")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	brewfile := filepath.Join(home, ".local/state/floe/recipe/Brewfile")
	restoreAt := slices.IndexFunc(f.Calls, func(c run.Cmd) bool { return len(c.Args) > 0 && c.Args[0] == "restore" })
	bundleAt := slices.IndexFunc(f.Calls, func(c run.Cmd) bool {
		return c.Name == brew && slices.Equal(c.Args, []string{"bundle", "install", "--file", brewfile})
	})
	if restoreAt < 0 || bundleAt < restoreAt {
		t.Fatalf("brew bundle must run after the restore: restore at %d, bundle at %d", restoreAt, bundleAt)
	}
}
