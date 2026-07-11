package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/run"
)

func TestStatusShowsSnapshotsAndSchedule(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	f.Handler = func(c run.Cmd) (string, error) {
		joined := strings.Join(c.Args, " ")
		switch {
		case strings.Contains(joined, "snapshots --json"):
			return `[{"time":"2026-07-10T13:00:00Z","short_id":"abc123","paths":["/Users/x"]}]`, nil
		case strings.Contains(joined, "stats"):
			return `{"total_size":8200000000}`, nil
		case c.Name == "security":
			return "pw\n", nil
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"1 snapshot", "abc123", "8.2 GB", "2026-07-10 1:00 PM", "daily at 1:00 PM"} {
		if !strings.Contains(s, want) {
			t.Fatalf("status output missing %q:\n%s", want, s)
		}
	}
}

func TestStatusWithoutConfigFails(t *testing.T) {
	f := &run.Fake{}
	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want actionable error without config")
	}
}

func TestStatusPropagatesResticError(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
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
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want error when restic snapshots fails")
	}
}

func TestStatusShowsNotScheduledWhenLaunchagentMissing(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	f.Handler = func(c run.Cmd) (string, error) {
		joined := strings.Join(c.Args, " ")
		switch {
		case strings.Contains(joined, "snapshots --json"):
			return `[]`, nil
		case len(c.Args) > 0 && c.Args[0] == "print":
			return "", errors.New("could not find service")
		case c.Name == "security":
			return "pw\n", nil
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "NOT SCHEDULED") {
		t.Fatalf("status output missing 'NOT SCHEDULED':\n%s", out.String())
	}
}

func TestStatusShowsDeferredRetentionWhenQuotaFails(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Destination = filepath.Join(os.Getenv("HOME"), "Library/Mobile Documents/com~apple~CloudDocs")
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	f.Handler = func(c run.Cmd) (string, error) {
		joined := strings.Join(c.Args, " ")
		switch {
		case strings.Contains(joined, "snapshots --json"):
			return `[{"time":"2026-07-10T13:00:00Z","short_id":"abc123","paths":["/Users/x"]}]`, nil
		case strings.Contains(joined, "stats"):
			return `{"total_size":8200000000}`, nil
		case c.Name == "security":
			return "pw\n", nil
		case c.Name == "brctl" && c.Args[0] == "quota":
			return "", errors.New("brctl unavailable")
		case c.Name == "brctl" && c.Args[0] == "status":
			return "r:8386 ld{synced-all} up:needs-upload uv:1", nil
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "deferred") {
		t.Fatalf("status output missing 'deferred':\n%s", out.String())
	}
}

func TestStatusHidesSyncStateWhenSyncPendingFails(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Destination = filepath.Join(os.Getenv("HOME"), "Library/Mobile Documents/com~apple~CloudDocs")
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	f.Handler = func(c run.Cmd) (string, error) {
		joined := strings.Join(c.Args, " ")
		switch {
		case strings.Contains(joined, "snapshots --json"):
			return `[{"time":"2026-07-10T13:00:00Z","short_id":"abc123","paths":["/Users/x"]}]`, nil
		case strings.Contains(joined, "stats"):
			return `{"total_size":8200000000}`, nil
		case c.Name == "security":
			return "pw\n", nil
		case c.Name == "brctl" && c.Args[0] == "quota":
			return "54022418096 bytes of quota remaining in personal account\n", nil
		case c.Name == "brctl" && c.Args[0] == "status":
			return "", errors.New("brctl status unavailable")
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "50.3 GiB quota remaining") {
		t.Fatalf("status output missing quota line:\n%s", s)
	}
	if strings.Contains(s, "sync in progress") || strings.Contains(s, "deferred") {
		t.Fatalf("status must claim nothing about sync state when it cannot be determined:\n%s", s)
	}
}

func TestStatusShowsICloudQuotaAndSync(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Destination = filepath.Join(os.Getenv("HOME"), "Library/Mobile Documents/com~apple~CloudDocs")
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	f.Handler = func(c run.Cmd) (string, error) {
		joined := strings.Join(c.Args, " ")
		switch {
		case strings.Contains(joined, "snapshots --json"):
			return `[{"time":"2026-07-10T13:00:00Z","short_id":"abc123","paths":["/Users/x"]}]`, nil
		case strings.Contains(joined, "stats"):
			return `{"total_size":8200000000}`, nil
		case c.Name == "security":
			return "pw\n", nil
		case c.Name == "brctl" && c.Args[0] == "quota":
			return "54022418096 bytes of quota remaining in personal account\n", nil
		case c.Name == "brctl" && c.Args[0] == "status":
			return "r:8386 ld{synced-all} up:needs-upload uv:1", nil
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"50.3 GiB quota remaining", "sync in progress", "deferred"} {
		if !strings.Contains(s, want) {
			t.Fatalf("status output missing %q:\n%s", want, s)
		}
	}
}
