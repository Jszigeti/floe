//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/backup"
	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/restic"
	"github.com/Jszigeti/floe/internal/run"
)

func TestFullBackupRestoreCycle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	bin, err := restic.EnsureBinary(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	home := os.Getenv("HOME")
	src := filepath.Join(home, "data")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "keep.txt"), []byte("v1"), 0o644)
	repo := filepath.Join(home, "dest/repo")
	os.MkdirAll(repo, 0o755)

	client := &restic.Client{Bin: bin, Repo: repo, Pass: "integration-pw", R: run.ExecRunner{}}
	if err := client.Init(ctx); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		Destination: filepath.Join(home, "dest"),
		Retention:   config.Retention{Daily: 7, Weekly: 4, Monthly: 6},
		Scope:       config.Scope{Paths: []string{src}},
	}
	d := backup.Deps{
		Cfg: cfg, Client: client, Runner: run.ExecRunner{},
		Notify: func(string, string) {}, Now: time.Now, Log: t.Logf,
	}
	if err := backup.Run(ctx, d); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(src, "keep.txt"), []byte("v2"), 0o644)
	if err := backup.Run(ctx, d); err != nil {
		t.Fatal(err)
	}

	snaps, err := client.Snapshots(ctx)
	if err != nil || len(snaps) != 2 {
		t.Fatalf("snapshots = %d (%v), want 2", len(snaps), err)
	}

	target := filepath.Join(home, "restored")
	if err := client.Restore(ctx, snaps[0].ShortID, target, ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(target, src, "keep.txt"))
	if err != nil || string(got) != "v1" {
		t.Fatalf("restored content = %q, err = %v, want v1 from first snapshot", got, err)
	}
}
