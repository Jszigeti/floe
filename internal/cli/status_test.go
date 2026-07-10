package cli

import (
	"bytes"
	"strings"
	"testing"

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
	for _, want := range []string{"1 snapshot", "abc123", "8.2 GB", "13:00"} {
		if !strings.Contains(s, want) {
			t.Fatalf("status output missing %q:\n%s", want, s)
		}
	}
}
