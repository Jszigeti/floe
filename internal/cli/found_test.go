package cli

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/run"
	"github.com/spf13/cobra"
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

func testPrompter(stdin string) (prompter, *bytes.Buffer) {
	cmd := &cobra.Command{}
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	return prompter{cmd: cmd, in: bufio.NewScanner(strings.NewReader(stdin))}, out
}

func TestFindBackupsNewestFirst(t *testing.T) {
	dest := t.TempDir()
	fakeRepo(t, dest, "Old-Mac", 30*24*time.Hour)
	fakeRepo(t, dest, "Air", 3*24*time.Hour)
	if err := os.MkdirAll(filepath.Join(dest, "Floe.backup", "Empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := findBackups(dest)
	if err != nil || len(found) != 2 || found[0].Name != "Air" || found[1].Name != "Old-Mac" {
		t.Fatalf("findBackups = %+v, %v", found, err)
	}
	if age := time.Since(found[0].LastSaved); age < 71*time.Hour || age > 73*time.Hour {
		t.Fatalf("last saved %v ago, want about 3 days", age)
	}
}

func TestFindBackupsWithoutFolder(t *testing.T) {
	found, err := findBackups(t.TempDir())
	if err != nil || len(found) != 0 {
		t.Fatalf("findBackups = %+v, %v", found, err)
	}
}

func TestChooseBackup(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name        string
		age         time.Duration
		stdin       string
		wantReplace bool
		wantErr     bool
	}{
		{"an old backup defaults to replace", 10 * 24 * time.Hour, "\n", true, false},
		{"a recent backup defaults to alongside", time.Hour, "\n", false, false},
		{"replacing a recent backup once the other Mac is stopped", time.Hour, "1\ny\n", true, false},
		{"replacing a recent backup stops otherwise", time.Hour, "1\n\n", false, true},
		{"a choice out of range", time.Hour, "7\n", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, out := testPrompter(tc.stdin)
			found := []foundBackup{{Name: "Air", Path: "/d/Floe.backup/Air", LastSaved: now.Add(-tc.age)}}
			b, replace, err := chooseBackup(p, found, now)
			if (err != nil) != tc.wantErr || replace != tc.wantReplace || (replace && b.Name != "Air") {
				t.Fatalf("chooseBackup = %+v, %v, %v\n%s", b, replace, err, out)
			}
			if !strings.Contains(out.String(), "Found a backup: Air, last saved "+now.Add(-tc.age).Format(time.DateOnly)+".") {
				t.Fatalf("question:\n%s", out)
			}
		})
	}
}

func TestChooseBackupListsEveryBackup(t *testing.T) {
	now := time.Now()
	p, out := testPrompter("2\n")
	found := []foundBackup{
		{Name: "Air", LastSaved: now.Add(-3 * 24 * time.Hour)},
		{Name: "Old-Mac", LastSaved: now.Add(-30 * 24 * time.Hour)},
	}
	b, replace, err := chooseBackup(p, found, now)
	if err != nil || !replace || b.Name != "Old-Mac" {
		t.Fatalf("chooseBackup = %+v, %v, %v", b, replace, err)
	}
	for _, want := range []string{"Found 2 backups.", "  2. Replace Old-Mac, last saved ", "  3. Add this Mac alongside: new backup, the others keep their own", "Choice [1]: "} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
