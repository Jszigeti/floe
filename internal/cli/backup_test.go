package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/run"
)

// setupHome writes a config, a fake restic binary and a keychain response into
// an isolated HOME, so cli commands can run end-to-end against a run.Fake.
func setupHome(t *testing.T, f *run.Fake) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cfg, _ := config.Default()
	cfg.Destination = filepath.Join(os.Getenv("HOME"), "dest")
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	installFakeRestic(t)
	if f.Responses == nil {
		f.Responses = map[string]run.Response{}
	}
	f.Responses["security find-generic-password -s floe -a restic -w"] = run.Response{Out: "pw\n"}
}

func TestBackupCommandRunsRestic(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetArgs([]string{"backup"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !f.CalledWith("backup", "--quiet") {
		t.Fatalf("calls = %+v", f.Calls)
	}
}

func TestBackupWithoutInitFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := Root(Deps{Version: "t", Runner: &run.Fake{}})
	cmd.SetArgs([]string{"backup"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want actionable error without config")
	}
}

func TestBuildClientFailsWhenResticBinaryMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg, _ := config.Default()
	cfg.Destination = filepath.Join(os.Getenv("HOME"), "dest")
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	// No restic binary written — buildClient must report the "floe init" hint.
	f := &run.Fake{Responses: map[string]run.Response{
		"security find-generic-password -s floe -a restic -w": {Out: "pw\n"},
	}}
	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetArgs([]string{"backup"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "floe init") {
		t.Fatalf("err = %v, want a hint to run floe init", err)
	}
}

func TestBackupCommandLogsFailure(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	f.Handler = func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "backup" {
			return "", errors.New("disk full")
		}
		if c.Name == "security" {
			return "pw\n", nil
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"backup"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want error propagated from backup.Run")
	}
	if !strings.Contains(out.String(), "backup failed") {
		t.Fatalf("output missing 'backup failed':\n%s", out.String())
	}
}

func TestBackupLogsTimestampedLines(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"backup"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	content := out.String()
	year := strconv.Itoa(time.Now().Year())
	for _, want := range []string{"backup started", "backup finished", year, "T"} {
		if !strings.Contains(content, want) {
			t.Fatalf("output missing %q:\n%s", want, content)
		}
	}
}
