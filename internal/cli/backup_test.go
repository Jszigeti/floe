package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/restic"
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
	bin, _ := restic.BinPath()
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("fake"), 0o755)
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
