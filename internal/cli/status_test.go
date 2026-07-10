package cli

import (
	"bytes"
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

func TestStatusShowsICloudQuotaAndSync(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	cfg, _ := config.Load()
	cfg.Destination = filepath.Join(os.Getenv("HOME"), "Library/Mobile Documents/com~apple~CloudDocs")
	cfg.Save()
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
	for _, want := range []string{"54.0 GB quota remaining", "sync in progress", "deferred"} {
		if !strings.Contains(s, want) {
			t.Fatalf("status output missing %q:\n%s", want, s)
		}
	}
}
