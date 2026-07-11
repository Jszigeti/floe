package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/config"
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

func TestDoctorChecksFullDiskAccess(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f) // Mail dir doesn't exist under an isolated HOME
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("healthy doctor must exit 0: %v\n%s", err, out.String())
	}
	s := out.String()
	if !strings.Contains(s, "full disk access") {
		t.Fatalf("doctor output missing full disk access check:\n%s", s)
	}
	if strings.Contains(s, "✗") {
		t.Fatalf("unexpected failure:\n%s", s)
	}
}

func TestInterpretFDAProbe(t *testing.T) {
	cases := []struct {
		name         string
		err          error
		wantOk, warn bool
	}{
		{"nil", nil, true, false},
		{"permission", fmt.Errorf("open: %w", fs.ErrPermission), true, true},
		{"not exist", fs.ErrNotExist, true, false},
		{"other", errors.New("other"), true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, warn, msg := interpretFDAProbe(c.err)
			if ok != c.wantOk || warn != c.warn {
				t.Fatalf("interpretFDAProbe(%v) = (%v, %v, %q), want ok=%v warn=%v", c.err, ok, warn, msg, c.wantOk, c.warn)
			}
		})
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

func icloudHandler(t *testing.T) func(run.Cmd) (string, error) {
	t.Helper()
	return func(c run.Cmd) (string, error) {
		joined := strings.Join(c.Args, " ")
		switch {
		case c.Name == "defaults" && strings.Contains(joined, "optimize-storage"):
			return "", errors.New("key missing")
		case c.Name == "brctl" && c.Args[0] == "quota":
			return "999999999999 bytes of quota remaining", nil
		case c.Name == "brctl" && c.Args[0] == "status":
			return "", nil
		case c.Name == "security":
			return "pw\n", nil
		}
		return "", nil
	}
}

func setupICloudHome(t *testing.T, f *run.Fake) config.Config {
	t.Helper()
	setupHome(t, f)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Destination = filepath.Join(os.Getenv("HOME"), "Library/Mobile Documents/com~apple~CloudDocs")
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestDoctorICloudChecksHealthy(t *testing.T) {
	f := &run.Fake{}
	cfg := setupICloudHome(t, f)
	repo, err := cfg.RepoPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	f.Handler = icloudHandler(t)
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("healthy doctor must exit 0: %v\n%s", err, out.String())
	}
	s := out.String()
	for _, want := range []string{"optimize mac storage", "all local", "quota", "GiB"} {
		if !strings.Contains(s, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "✗") {
		t.Fatalf("unexpected failure:\n%s", s)
	}
}

func TestDoctorWarnsWhenDatalessScanFails(t *testing.T) {
	f := &run.Fake{}
	setupICloudHome(t, f) // repo directory intentionally not created
	f.Handler = icloudHandler(t)
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("scan-failure warn must not fail doctor: %v\n%s", err, out.String())
	}
	s := out.String()
	if !strings.Contains(s, "could not scan for evicted files") {
		t.Fatalf("doctor output missing scan-failure message:\n%s", s)
	}
	if !strings.Contains(s, "!") {
		t.Fatalf("doctor output missing warn marker:\n%s", s)
	}
}
