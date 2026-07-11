package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/run"
)

func snapshotFake(t *testing.T) *run.Fake {
	f := &run.Fake{}
	setupHome(t, f)
	f.Handler = func(c run.Cmd) (string, error) {
		if strings.Contains(strings.Join(c.Args, " "), "snapshots --json") {
			return `[{"time":"2026-07-10T13:00:00Z","short_id":"abc123","paths":["/Users/x"]}]`, nil
		}
		if c.Name == "security" {
			return "pw\n", nil
		}
		return "", nil
	}
	return f
}

func TestSnapshotsListsHistory(t *testing.T) {
	f := snapshotFake(t)
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"snapshots"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "abc123") || !strings.Contains(out.String(), "2026-07-10 1:00 PM") {
		t.Fatalf("out = %q", out.String())
	}
}

func TestSnapshotsWithoutConfigFails(t *testing.T) {
	f := &run.Fake{}
	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetArgs([]string{"snapshots"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want actionable error without config")
	}
}

func TestSnapshotsPropagatesResticError(t *testing.T) {
	f := snapshotFake(t)
	f.Handler = func(c run.Cmd) (string, error) {
		if strings.Contains(strings.Join(c.Args, " "), "snapshots --json") {
			return "", errors.New("repo unreachable")
		}
		if c.Name == "security" {
			return "pw\n", nil
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetArgs([]string{"snapshots"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want error when restic snapshots fails")
	}
}

func TestSnapshotsEmptyList(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	f.Handler = func(c run.Cmd) (string, error) {
		if strings.Contains(strings.Join(c.Args, " "), "snapshots --json") {
			return "[]", nil
		}
		if c.Name == "security" {
			return "pw\n", nil
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"snapshots"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No snapshots yet") {
		t.Fatalf("out = %q, want empty-state message", out.String())
	}
}

func TestRestoreWithoutConfigFails(t *testing.T) {
	f := &run.Fake{}
	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetArgs([]string{"restore", "latest"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want actionable error without config")
	}
}

func TestRestorePropagatesResticError(t *testing.T) {
	f := snapshotFake(t)
	f.Handler = func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "restore" {
			return "", errors.New("target unwritable")
		}
		if strings.Contains(strings.Join(c.Args, " "), "snapshots --json") {
			return `[]`, nil
		}
		if c.Name == "security" {
			return "pw\n", nil
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetArgs([]string{"restore", "latest"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want error when restic restore fails")
	}
}

func TestRestoreBuildsResticArgs(t *testing.T) {
	f := snapshotFake(t)
	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetArgs([]string{"restore", "abc123", "/Users/x/doc.txt", "--target", "/tmp/out"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !f.CalledWith("restore", "abc123", "--target /tmp/out", "--include /Users/x/doc.txt") {
		t.Fatalf("calls = %+v", f.Calls)
	}
}

func TestRestoreDefaultTarget(t *testing.T) {
	f := snapshotFake(t)
	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetArgs([]string{"restore", "latest"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !f.CalledWith("restore", "latest", "--target", "floe-restore-latest") {
		t.Fatalf("calls = %+v", f.Calls)
	}
}
