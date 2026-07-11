package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/launchd"
	"github.com/Jszigeti/floe/internal/run"
)

func TestUninstallRemovesSchedule(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Pre-write a plist via launchd.Install with a fake.
	fakeInstall := &run.Fake{}
	if err := launchd.Install(context.Background(), fakeInstall, "/bin/floe", 13); err != nil {
		t.Fatal(err)
	}
	pp, _ := launchd.PlistPath()
	if _, err := os.Stat(pp); err != nil {
		t.Fatal("plist not written after install")
	}

	// Run the uninstall command with a new fake.
	fakeUninstall := &run.Fake{}
	cmd := Root(Deps{Version: "t", Runner: fakeUninstall})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"uninstall"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	// Check no error, fake called launchctl bootout, plist gone, output correct.
	if !fakeUninstall.CalledWith("launchctl", "bootout") {
		t.Fatalf("want launchctl bootout call, got %+v", fakeUninstall.Calls)
	}
	if _, err := os.Stat(pp); err == nil {
		t.Fatal("plist still exists after uninstall")
	}
	output := out.String()
	if !strings.Contains(output, "schedule removed") {
		t.Fatalf("output missing 'schedule removed': %s", output)
	}
}

func TestUninstallPropagatesRemoveError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pp, err := launchd.PlistPath()
	if err != nil {
		t.Fatal(err)
	}
	// Make the plist path a non-empty directory so os.Remove fails.
	if err := os.MkdirAll(pp, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pp, "child"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := Root(Deps{Version: "t", Runner: &run.Fake{}})
	cmd.SetArgs([]string{"uninstall"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want error when the plist path cannot be removed")
	}
}
