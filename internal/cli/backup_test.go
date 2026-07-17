package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/backup"
	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/run"
)

// setupHome writes a config, a fake restic binary and a keychain response into
// an isolated HOME, so cli commands can run end-to-end against a run.Fake.
func setupHome(t *testing.T, f *run.Fake) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
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
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
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
	err = cmd.Execute()
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

func TestBackupLogsFailedNotification(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.Default() // default destination is the iCloud Drive path
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	installFakeRestic(t)
	f.Handler = func(c run.Cmd) (string, error) {
		switch {
		case c.Name == "osascript":
			return "", errors.New("notification center unavailable")
		case c.Name == "security":
			return "pw\n", nil
		case c.Name == "brctl" && len(c.Args) > 0 && c.Args[0] == "quota":
			return "1000000000 bytes of quota remaining in personal account", nil // below the 5 GiB low-quota threshold
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"backup"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("backup failed: %v\noutput:\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "notification failed") {
		t.Fatalf("output missing 'notification failed':\n%s", out.String())
	}
}

// TestBackupHonorsScheduledEnv proves FLOE_SCHEDULED reaches backup.Deps.Scheduled
// through the real command, observed via behavior: only a scheduled run owns the
// incomplete-backup report (see internal/backup.logIncomplete).
func TestBackupHonorsScheduledEnv(t *testing.T) {
	// The stderr line must match restic.unreadablePathRe (split "err"+"or:" so this
	// source line itself never matches it) so IncompleteError.Unreadable is non-empty —
	// otherwise logIncomplete takes the raw-detail fallback and never writes a report.
	script := `printf 'err''or: open /Users/x/unreadable: operation not permitted\n' >&2; exit 3`
	_, exit3Err := run.ExecRunner{}.Run(context.Background(), run.Cmd{Name: "sh", Args: []string{"-c", script}})
	if exit3Err == nil {
		t.Fatal("want exit 3 to produce an error")
	}

	tests := []struct {
		name       string
		setEnv     bool
		env        string
		wantReport bool
	}{
		{name: "scheduled", setEnv: true, env: "1", wantReport: true},
		{name: "unset", setEnv: false, wantReport: false},
		{name: "zero", setEnv: true, env: "0", wantReport: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &run.Fake{}
			setupHome(t, f)
			if tt.setEnv {
				t.Setenv("FLOE_SCHEDULED", tt.env)
			} else if orig, ok := os.LookupEnv("FLOE_SCHEDULED"); ok {
				_ = os.Unsetenv("FLOE_SCHEDULED")
				t.Cleanup(func() { _ = os.Setenv("FLOE_SCHEDULED", orig) })
			}
			f.Handler = func(c run.Cmd) (string, error) {
				if len(c.Args) > 0 && c.Args[0] == "backup" {
					return "", exit3Err
				}
				if c.Name == "security" {
					return "pw\n", nil
				}
				return "", nil
			}
			cmd := Root(Deps{Version: "t", Runner: f})
			cmd.SetArgs([]string{"backup"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("incomplete backup must not fail Execute: %v", err)
			}

			logPath, err := backup.IncompleteLogPath()
			if err != nil {
				t.Fatal(err)
			}
			_, statErr := os.Stat(logPath)
			gotReport := statErr == nil
			if gotReport != tt.wantReport {
				t.Errorf("report exists = %v, want %v (stat err: %v)", gotReport, tt.wantReport, statErr)
			}
		})
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
