package launchd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/run"
)

func TestPlistContainsScheduleAndBinary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p, err := Plist("/usr/local/bin/floe", 13)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<string>com.floe.backup</string>",
		"<string>/usr/local/bin/floe</string>",
		"<string>backup</string>",
		"<key>Hour</key><integer>13</integer>",
		"floe.log",
		"FLOE_SCHEDULED",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("plist missing %q:\n%s", want, p)
		}
	}
}

func TestPlistPathHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := PlistPath(); err == nil {
		t.Fatal("want error when HOME is unresolvable")
	}
}

func TestLogPathHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := LogPath(); err == nil {
		t.Fatal("want error when HOME is unresolvable")
	}
}

func TestPlistHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := Plist("/bin/floe", 13); err == nil {
		t.Fatal("want error when HOME is unresolvable")
	}
}

func TestInstallHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	if err := Install(context.Background(), &run.Fake{}, "/bin/floe", 13); err == nil {
		t.Fatal("want error when HOME is unresolvable")
	}
}

func TestInstallWriteFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Block Library/LaunchAgents with a regular file so MkdirAll fails.
	if err := os.MkdirAll(filepath.Join(home, "Library"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "Library/LaunchAgents"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), &run.Fake{}, "/bin/floe", 13); err == nil {
		t.Fatal("want error when the plist directory cannot be created")
	}
}

func TestInstallWriteFileFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "Library/LaunchAgents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if err := Install(context.Background(), &run.Fake{}, "/bin/floe", 13); err == nil {
		t.Fatal("want error when the plist file cannot be written")
	}
}

func TestInstallBootstrapErrorPropagates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "bootstrap" {
			return "", errors.New("bootstrap failed")
		}
		return "", nil
	}}
	if err := Install(context.Background(), f, "/bin/floe", 13); err == nil {
		t.Fatal("want bootstrap error propagated")
	}
}

func TestUninstallHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	if err := Uninstall(context.Background(), &run.Fake{}); err == nil {
		t.Fatal("want error when HOME is unresolvable")
	}
}

func TestUninstallNonNotExistRemoveError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	pp, err := PlistPath()
	if err != nil {
		t.Fatal(err)
	}
	// Make the plist path a non-empty directory so os.Remove fails with something
	// other than fs.ErrNotExist.
	if err := os.MkdirAll(pp, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pp, "child"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(context.Background(), &run.Fake{}); err == nil {
		t.Fatal("want error when the plist path cannot be removed")
	}
}

func TestInstallWritesPlistAndBootstraps(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := &run.Fake{}
	if err := Install(context.Background(), f, "/bin/floe", 13); err != nil {
		t.Fatal(err)
	}
	pp, _ := PlistPath()
	if _, err := os.Stat(pp); err != nil {
		t.Fatal("plist not written")
	}
	uid := fmt.Sprintf("gui/%d", os.Getuid())
	if !f.CalledWith("launchctl", "bootout", uid) || !f.CalledWith("launchctl", "bootstrap", uid, pp) {
		t.Fatalf("calls = %+v", f.Calls)
	}
}

func TestIsLoaded(t *testing.T) {
	loaded := &run.Fake{}
	if !IsLoaded(context.Background(), loaded) {
		t.Fatal("want loaded when print succeeds")
	}
	uid := fmt.Sprintf("gui/%d", os.Getuid())
	if !loaded.CalledWith("launchctl", "print", uid+"/"+Label) {
		t.Fatalf("want launchctl print call with %q, got %+v", uid+"/"+Label, loaded.Calls)
	}
	notLoaded := &run.Fake{Default: run.Response{Err: fmt.Errorf("could not find service")}}
	if IsLoaded(context.Background(), notLoaded) {
		t.Fatal("want not loaded on error")
	}
}

func TestUninstallRemovesPlistAndBootsOut(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	boot := &run.Fake{}
	if err := Install(context.Background(), boot, "/bin/floe", 13); err != nil {
		t.Fatal(err)
	}
	pp, _ := PlistPath()
	if _, err := os.Stat(pp); err != nil {
		t.Fatal("plist not written after install")
	}
	uid := fmt.Sprintf("gui/%d", os.Getuid())
	unboot := &run.Fake{}
	if err := Uninstall(context.Background(), unboot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pp); err == nil {
		t.Fatal("plist still exists after uninstall")
	}
	if !unboot.CalledWith("launchctl", "bootout", uid) {
		t.Fatalf("want launchctl bootout, got %+v", unboot.Calls)
	}
	if err := Uninstall(context.Background(), unboot); err != nil {
		t.Fatalf("uninstall must be idempotent, got error: %v", err)
	}
}
