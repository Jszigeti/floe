package launchd

import (
	"context"
	"fmt"
	"os"
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
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("plist missing %q:\n%s", want, p)
		}
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
