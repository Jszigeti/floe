package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/run"
)

// fakeRepo creates a repository folder in dest whose newest snapshot file is age old.
func fakeRepo(t *testing.T, dest, name string, age time.Duration) string {
	t.Helper()
	repo := filepath.Join(dest, "Floe.backup", name)
	snap := filepath.Join(repo, "snapshots", "0123abcd")
	if err := os.MkdirAll(filepath.Dir(snap), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snap, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(snap, when, when); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestFreeRepoPathSkipsExistingFolders(t *testing.T) {
	dest := t.TempDir()
	name, err := config.MachineName()
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(dest, "Floe.backup", name)
	for _, want := range []string{base, base + "-2", base + "-3"} {
		got, err := freeRepoPath(dest)
		if err != nil || got != want {
			t.Fatalf("freeRepoPath = %q, %v, want %q", got, err, want)
		}
		if err := os.MkdirAll(got, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOwnRepoBelongsToThisMacOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dest := t.TempDir()
	own := filepath.Join(dest, "Floe.backup", "Old-Name")
	cfg := config.Config{Destination: dest, Repo: own, Machine: thisMac}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if got := ownRepo(dest, thisMac); got != own {
		t.Fatalf("own Mac: ownRepo = %q, want %q", got, own)
	}
	if got := ownRepo(dest, "OTHER-MAC"); got != "" {
		t.Fatalf("other Mac: ownRepo = %q, want none", got)
	}
	if got := ownRepo(t.TempDir(), thisMac); got != "" {
		t.Fatalf("other destination: ownRepo = %q, want none", got)
	}
	cfg.Machine = "" // a config from before v0.2.0 belongs to the Mac running it
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if got := ownRepo(dest, thisMac); got != own {
		t.Fatalf("old config: ownRepo = %q, want %q", got, own)
	}
}

func TestInitNeverReusesAnExistingFolder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	installFakeRestic(t)
	dest := filepath.Join(os.Getenv("HOME"), "dest")
	name, err := config.MachineName()
	if err != nil {
		t.Fatal(err)
	}
	taken := filepath.Join(dest, "Floe.backup", name)
	if err := os.MkdirAll(taken, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := runInit(&run.Fake{Handler: freshInitHandler}, dest+"\n\n\n"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Repo != taken+"-2" {
		t.Fatalf("repo = %q, want %q", cfg.Repo, taken+"-2")
	}
}

func TestInitKeepsThisMacsRepo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	installFakeRestic(t)
	dest := filepath.Join(os.Getenv("HOME"), "dest")
	own := fakeRepo(t, dest, "Old-Name", 10*24*time.Hour)
	fakeRepo(t, dest, "Other-Mac", 10*24*time.Hour)
	prev, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	prev.Destination, prev.Repo, prev.Machine = dest, own, thisMac
	if err := prev.Save(); err != nil {
		t.Fatal(err)
	}
	out, err := runInit(&run.Fake{Handler: freshInitHandler}, dest+"\n\n\n")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if strings.Contains(out, "Found") {
		t.Fatalf("a Mac re-running init on its own config must not be asked about other backups:\n%s", out)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Repo != own {
		t.Fatalf("repo = %q, want %q", cfg.Repo, own)
	}
}
