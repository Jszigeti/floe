package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/run"
)

func TestDoctorAllHealthy(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f) // non-iCloud destination → iCloud checks skipped
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("healthy doctor must exit 0: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "✗") {
		t.Fatalf("unexpected failure:\n%s", out.String())
	}
}

func TestDoctorFailsWithoutKeychain(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	f.Responses["security find-generic-password -s floe -a restic -w"] =
		run.Response{Err: errors.New("not found")}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("doctor must exit non-zero when a check fails")
	}
	if !strings.Contains(out.String(), "✗") {
		t.Fatalf("want failed check marker:\n%s", out.String())
	}
}
