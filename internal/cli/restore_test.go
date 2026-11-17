package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/run"
)

func TestRestoreLatestMeansTheNewestHomeSnapshot(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	f.Handler = func(c run.Cmd) (string, error) {
		switch {
		case strings.Contains(strings.Join(c.Args, " "), "snapshots --json"):
			return `[{"time":"2026-07-10T13:00:00Z","short_id":"home01"},{"time":"2026-07-11T13:00:00Z","short_id":"apps01","tags":["apps"]}]`, nil
		case c.Name == "security":
			return "pw\n", nil
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"restore", "latest", "--target", t.TempDir()})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !f.CalledWith("restore", "home01") || f.CalledWith("restore", "latest") {
		t.Fatalf("want the newest home snapshot restored, calls: %+v", f.Calls)
	}
}
