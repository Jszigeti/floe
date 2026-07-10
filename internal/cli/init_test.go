package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/launchd"
	"github.com/Jszigeti/floe/internal/restic"
	"github.com/Jszigeti/floe/internal/run"
)

func TestInitWizardEndToEnd(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	// pre-install fake restic so EnsureBinary skips the network
	bin, _ := restic.BinPath()
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("fake"), 0o755)
	// keychain lookup fails → wizard must generate and store a new passphrase
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "security" && c.Args[0] == "find-generic-password" {
			return "", os.ErrNotExist
		}
		if len(c.Args) > 1 && c.Args[0] == "cat" { // IsInitialized → false
			return "", os.ErrNotExist
		}
		return "", nil
	}
	dest := filepath.Join(home, "dest")
	os.MkdirAll(dest, 0o755)

	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	// answers: destination, exclusions (none), hour (default)
	cmd.SetIn(strings.NewReader(dest + "\n\n\n"))
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v\noutput:\n%s", err, out.String())
	}

	cfg, err := config.Load()
	if err != nil || cfg.Destination != dest {
		t.Fatalf("cfg = %+v, err = %v", cfg, err)
	}
	if !f.CalledWith("security", "add-generic-password") {
		t.Fatal("passphrase not stored in keychain")
	}
	if !f.CalledWith("restic", "init") && !f.CalledWith("init") {
		t.Fatalf("repo not initialized; calls = %+v", f.Calls)
	}
	if pp, _ := launchd.PlistPath(); !fileExists(pp) {
		t.Fatal("launchagent not installed")
	}
	if !f.CalledWith("backup", "--quiet") {
		t.Fatal("first backup not run")
	}
	if !strings.Contains(out.String(), "password manager") {
		t.Fatal("passphrase warning not shown")
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
