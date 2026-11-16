package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/replace"
	"github.com/Jszigeti/floe/internal/run"
)

func TestReplaceResumesPinnedSnapshot(t *testing.T) {
	_, dest, _ := startReplace(t)
	repo := fakeRepo(t, dest, "Old-Mac", 10*24*time.Hour)
	if err := (replace.State{Backup: "Old-Mac", Repo: repo, SnapshotID: "aaa111", OldHome: "/Users/alice"}).Save(); err != nil {
		t.Fatal(err)
	}
	f := replaceFake(nil, "1000") // its newest snapshot is bbb222
	out, err := runInit(f, "\npw\n")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Resume replacing Old-Mac? [Y/n]: ") || strings.Contains(out, "Backup destination") {
		t.Fatalf("want the resume offer instead of the wizard:\n%s", out)
	}
	c := resticCall(f, "restore")
	if c == nil || !slices.Contains(c.Args, "aaa111:/Users/alice") {
		t.Fatalf("restore call = %+v, want the pinned snapshot", c)
	}
	cfg, err := config.Load()
	if err != nil || cfg.Repo != repo || cfg.Destination != dest {
		t.Fatalf("config = %+v, %v", cfg, err)
	}
	if _, ok, _ := replace.LoadState(); ok {
		t.Fatal("state left behind by a finished replace")
	}
}

func TestReplaceKeepsItsStateUntilItEnds(t *testing.T) {
	_, dest, repo := startReplace(t)
	f := replaceFake(nil, "1000")
	base := f.Handler
	open := true
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "osascript" && slices.Contains(c.Args, "JavaScript") && open {
			return "Safari\n", nil
		}
		return base(c)
	}
	if _, err := runInit(f, dest+"\n\npw\n"); err == nil {
		t.Fatal("want a stop while Safari is open")
	}
	st, ok, err := replace.LoadState()
	if err != nil || !ok || st.Backup != "MacBook-Air" || st.Repo != repo || st.SnapshotID != "bbb222" || st.OldHome != "/Users/alice" || st.Started.IsZero() {
		t.Fatalf("state = %+v, %v, %v, want the pinned snapshot", st, ok, err)
	}
	open = false
	out, err := runInit(f, "y\npw\n")
	if err != nil || !strings.Contains(out, "Resume replacing MacBook-Air?") {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if _, ok, _ := replace.LoadState(); ok {
		t.Fatal("state left behind by a finished replace")
	}
}

func TestInitForgetsADeclinedResume(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	installFakeRestic(t)
	dest := filepath.Join(os.Getenv("HOME"), "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (replace.State{Backup: "Old-Mac", Repo: filepath.Join(dest, "Floe.backup/Old-Mac"), SnapshotID: "aaa111"}).Save(); err != nil {
		t.Fatal(err)
	}
	f := &run.Fake{Handler: freshInitHandler}
	out, err := runInit(f, "n\n"+dest+"\n\n\n")
	if err != nil || !strings.Contains(out, "Backup destination") {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if resticCall(f, "restore") != nil {
		t.Fatal("restored after the user declined")
	}
	if _, ok, _ := replace.LoadState(); ok {
		t.Fatal("a declined replace must be forgotten")
	}
}

func TestReplaceResumeReusesTheStoredPassphrase(t *testing.T) {
	_, dest, repo := startReplace(t)
	if err := (replace.State{Backup: "MacBook-Air", Repo: repo, SnapshotID: "aaa111", OldHome: "/Users/alice"}).Save(); err != nil {
		t.Fatal(err)
	}
	f := replaceFake(nil, "1000")
	base := f.Handler
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "security" && c.Args[0] == "find-generic-password" {
			return "pw\n", nil
		}
		return base(c)
	}
	out, err := runInit(f, "\n")
	if err != nil || strings.Contains(out, "Passphrase of") {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if cfg, err := config.Load(); err != nil || cfg.Destination != dest {
		t.Fatalf("config = %+v, %v", cfg, err)
	}
}
