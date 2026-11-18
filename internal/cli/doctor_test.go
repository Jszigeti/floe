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
	"time"

	"github.com/Jszigeti/floe/internal/backup"
	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/launchd"
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

func TestDoctorWithoutConfigFails(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", t.TempDir())
	cmd := Root(Deps{Version: "t", Runner: f})
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want actionable error without config")
	}
}

func TestDoctorFailsWhenResticBinaryMissing(t *testing.T) {
	f := &run.Fake{}
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Destination = filepath.Join(os.Getenv("HOME"), "dest")
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	// No restic binary written.
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("doctor must exit non-zero when the restic binary is missing")
	}
	s := out.String()
	if !strings.Contains(s, "run `floe init`") {
		t.Fatalf("doctor output missing floe init hint:\n%s", s)
	}
	if !strings.Contains(s, "✗") {
		t.Fatalf("doctor output missing failure marker:\n%s", s)
	}
}

func TestDoctorFailsWhenScheduleNotLoaded(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	f.Handler = func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "print" {
			return "", errors.New("could not find service")
		}
		if c.Name == "security" {
			return "pw\n", nil
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("doctor must exit non-zero when the schedule is not loaded")
	}
	if !strings.Contains(out.String(), "launchagent not loaded") {
		t.Fatalf("doctor output missing schedule failure:\n%s", out.String())
	}
}

func TestDoctorWarnsWhenScheduleMissingMarker(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	pp, err := launchd.PlistPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(pp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pp, []byte("<plist></plist>"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("a schedule predating this version must not fail doctor: %v\n%s", err, out.String())
	}
	s := out.String()
	if !strings.Contains(s, "schedule predates this version") {
		t.Fatalf("doctor output missing predates-version warning:\n%s", s)
	}
	if !strings.Contains(s, "! schedule") {
		t.Fatalf("doctor output missing warn marker on schedule check:\n%s", s)
	}
}

func TestDoctorOkWhenScheduleHasMarker(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	pp, err := launchd.PlistPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(pp), 0o755); err != nil {
		t.Fatal(err)
	}
	content, err := launchd.Plist("/usr/local/bin/floe", 13)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pp, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("healthy doctor must exit 0: %v\n%s", err, out.String())
	}
	s := out.String()
	if strings.Contains(s, "predates this version") {
		t.Fatalf("doctor output should not warn when the plist has the marker:\n%s", s)
	}
	if !strings.Contains(s, "daily at") {
		t.Fatalf("doctor output missing plain schedule OK line:\n%s", s)
	}
}

func TestDoctorFailsWhenRepositoryUnreachable(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	f.Handler = func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "cat" {
			return "", errors.New("repo unreachable")
		}
		if c.Name == "security" {
			return "pw\n", nil
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("doctor must exit non-zero when the repository is unreachable")
	}
	if !strings.Contains(out.String(), "repo unreachable") {
		t.Fatalf("doctor output missing repository execution error:\n%s", out.String())
	}
}

func TestDoctorFailsWhenRepositoryNotInitialized(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	f.Handler = func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "cat" {
			return "", errRepoMissing
		}
		if c.Name == "security" {
			return "pw\n", nil
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("doctor must exit non-zero when the repository is not initialized")
	}
	if !strings.Contains(out.String(), "unreachable or not initialized") {
		t.Fatalf("doctor output missing repository failure:\n%s", out.String())
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

func TestLastIncompleteReport(t *testing.T) {
	cases := []struct {
		name      string
		write     bool
		content   string
		wantCount int
		wantErr   bool
	}{
		{"missing file", false, "", 0, true},
		{"one path", true, "one\n", 1, false},
		{"three paths", true, "a\nb\nc\n", 3, false},
		{"empty file", true, "", 0, false},
		{"whitespace only", true, "   \n\t\n", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			if c.write {
				p, err := backup.IncompleteLogPath()
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(c.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			count, when, err := lastIncompleteReport()
			if c.wantErr {
				if err == nil {
					t.Fatal("want an error for a missing state file")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if count != c.wantCount {
				t.Fatalf("count = %d, want %d", count, c.wantCount)
			}
			if when.IsZero() {
				t.Fatal("want a non-zero mtime")
			}
		})
	}
}

func TestDoctorReportsCompleteWhenNoIncompleteLog(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("healthy doctor must exit 0: %v\n%s", err, out.String())
	}
	s := out.String()
	if !strings.Contains(s, "last scheduled backup") || !strings.Contains(s, "complete") {
		t.Fatalf("doctor output missing complete last-scheduled-backup line:\n%s", s)
	}
}

func TestDoctorWarnsWhenLastScheduledBackupSkippedFiles(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	logPath, err := backup.IncompleteLogPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("a skipped-files warning must not fail doctor: %v\n%s", err, out.String())
	}
	s := out.String()
	if !strings.Contains(s, "skipped 3 protected files") {
		t.Fatalf("doctor output missing skipped-file count:\n%s", s)
	}
	if !strings.Contains(s, "!") {
		t.Fatalf("doctor output missing warn marker:\n%s", s)
	}
}

func TestHostAppHint(t *testing.T) {
	cases := []struct {
		name, termProgram, want string
	}{
		{"apple terminal", "Apple_Terminal", "Terminal"},
		{"iterm", "iTerm.app", "iTerm"},
		{"vscode", "vscode", "your editor's app (VS Code or Cursor)"},
		{"unknown", "SomeOtherApp", "your terminal app"},
		{"empty", "", "your terminal app"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("TERM_PROGRAM", c.termProgram)
			if got := hostAppHint(); got != c.want {
				t.Fatalf("hostAppHint() = %q, want %q", got, c.want)
			}
		})
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
			if c.name == "permission" && !strings.Contains(msg, "Full Disk Access to "+hostAppHint()) {
				t.Fatalf("interpretFDAProbe permission msg = %q, want it to name the host app", msg)
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

func TestDoctorWarnsWhenOptimizeStorageOn(t *testing.T) {
	f := &run.Fake{}
	cfg := setupICloudHome(t, f)
	repo, err := cfg.RepoPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	f.Handler = func(c run.Cmd) (string, error) {
		joined := strings.Join(c.Args, " ")
		switch {
		case c.Name == "defaults" && strings.Contains(joined, "optimize-storage"):
			return "1\n", nil
		case c.Name == "brctl" && c.Args[0] == "quota":
			return "999999999999 bytes of quota remaining", nil
		case c.Name == "brctl" && c.Args[0] == "status":
			return "", nil
		case c.Name == "security":
			return "pw\n", nil
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("optimize-storage warn must not fail doctor: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "may evict repo files") {
		t.Fatalf("doctor output missing optimize-storage warning:\n%s", out.String())
	}
}

func TestDoctorWarnsWhenQuotaUnknown(t *testing.T) {
	f := &run.Fake{}
	cfg := setupICloudHome(t, f)
	repo, err := cfg.RepoPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	f.Handler = func(c run.Cmd) (string, error) {
		joined := strings.Join(c.Args, " ")
		switch {
		case c.Name == "defaults" && strings.Contains(joined, "optimize-storage"):
			return "", errors.New("key missing")
		case c.Name == "brctl" && c.Args[0] == "quota":
			return "", errors.New("brctl unavailable")
		case c.Name == "brctl" && c.Args[0] == "status":
			return "", nil
		case c.Name == "security":
			return "pw\n", nil
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unknown quota must not fail doctor: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "icloud quota") || !strings.Contains(out.String(), "unknown") {
		t.Fatalf("doctor output missing unknown-quota line:\n%s", out.String())
	}
}

func TestDoctorWarnsWhenQuotaLow(t *testing.T) {
	f := &run.Fake{}
	cfg := setupICloudHome(t, f)
	repo, err := cfg.RepoPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	f.Handler = func(c run.Cmd) (string, error) {
		joined := strings.Join(c.Args, " ")
		switch {
		case c.Name == "defaults" && strings.Contains(joined, "optimize-storage"):
			return "", errors.New("key missing")
		case c.Name == "brctl" && c.Args[0] == "quota":
			return "1000000 bytes of quota remaining", nil
		case c.Name == "brctl" && c.Args[0] == "status":
			return "", nil
		case c.Name == "security":
			return "pw\n", nil
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("low quota must not fail doctor: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "GiB remaining") {
		t.Fatalf("doctor output missing low-quota line:\n%s", out.String())
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

func writeScheduledLog(t *testing.T, content string) {
	t.Helper()
	p, err := launchd.LogPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLastScheduledFailure(t *testing.T) {
	cases := []struct {
		name       string
		log        string
		wantFailed bool
		wantWhen   string
	}{
		{"no log", "", false, ""},
		{"last run finished", "2026-09-19T13:00:01+02:00 backup started\n2026-09-19T13:05:00+02:00 backup finished\n", false, ""},
		{"last run failed", "2026-09-19T13:05:00+02:00 backup finished\n2026-09-20T13:06:32+02:00 backup failed: restic: exit status 10: repository does not exist\nIs there a repository at the following location?\n", true, "2026-09-20T13:06:32+02:00"},
		{"failure then success", "2026-09-20T13:06:32+02:00 backup failed: boom\n2026-09-21T13:04:00+02:00 backup finished\n", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			if c.log != "" {
				writeScheduledLog(t, c.log)
			}
			when, failed := lastScheduledFailure()
			if failed != c.wantFailed {
				t.Fatalf("failed = %v, want %v", failed, c.wantFailed)
			}
			if c.wantFailed && when.Format(time.RFC3339) != c.wantWhen {
				t.Fatalf("when = %s, want %s", when.Format(time.RFC3339), c.wantWhen)
			}
		})
	}
}

func TestDoctorFailsWhenLastScheduledBackupFailed(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	writeScheduledLog(t, "2026-09-20T13:06:32+02:00 backup failed: boom\n")
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("a failed scheduled backup must fail doctor:\n%s", out.String())
	}
	if s := out.String(); !strings.Contains(s, "failed on 2026-09-20 1:06 PM") {
		t.Fatalf("doctor output missing the failed scheduled run:\n%s", s)
	}
}

func TestLastScheduledFailureIgnoresFailuresBeforeReinstall(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeScheduledLog(t, "2026-09-20T13:06:32+02:00 backup failed: boom\n")
	pp, err := launchd.PlistPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(pp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pp, []byte("plist"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, failed := lastScheduledFailure(); failed {
		t.Fatal("a failure older than the installed schedule must not count")
	}
}

func TestDoctorIgnoresIncompleteReportBeforeReinstall(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	logPath, err := backup.IncompleteLogPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(logPath, old, old); err != nil {
		t.Fatal(err)
	}
	pp, err := launchd.PlistPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(pp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pp, []byte("FLOE_SCHEDULED"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	_ = cmd.Execute() // the verdict under test is the line below, not doctor's overall exit
	if s := out.String(); strings.Contains(s, "skipped 3 protected files") {
		t.Fatalf("a report older than the installed schedule must not count:\n%s", s)
	}
}

func TestDoctorReportsIncompleteRecipe(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	dir := filepath.Join(os.Getenv("HOME"), ".local/state/floe/manifest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	status := `{"failed": {"Brewfile": "2026-11-04T12:00:00Z"}}`
	if err := os.WriteFile(filepath.Join(dir, "status.json"), []byte(status), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("an incomplete recipe is a warning, not a failure: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "! recipe") || !strings.Contains(out.String(), "recipe incomplete since 2026-11-04: Brewfile failed") {
		t.Fatalf("want the incomplete recipe reported:\n%s", out.String())
	}
}

func TestDoctorRecipeCompleteAndUnreadable(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	run := func() string {
		cmd := Root(Deps{Version: "t", Runner: f})
		out := new(bytes.Buffer)
		cmd.SetOut(out)
		cmd.SetArgs([]string{"doctor"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("doctor: %v\n%s", err, out.String())
		}
		return out.String()
	}
	if out := run(); !strings.Contains(out, "✓ recipe") {
		t.Fatalf("want a complete recipe without status.json:\n%s", out)
	}
	dir := filepath.Join(os.Getenv("HOME"), ".local/state/floe/manifest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "status.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := run(); !strings.Contains(out, "! recipe") {
		t.Fatalf("want a warning for an unreadable status.json:\n%s", out)
	}
}

func TestDoctorFailsWhenNoBackupRanForTwoDays(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	old := time.Now().Add(-75 * time.Hour).UTC().Format(time.RFC3339)
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "security" {
			return "pw\n", nil
		}
		if len(c.Args) > 0 && c.Args[0] == "snapshots" {
			return `[{"time":"` + old + `","short_id":"aaa111","paths":["/Users/x"],"tags":["home"]}]`, nil
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("doctor must exit non-zero when no backup ran for days:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "no backup since") {
		t.Fatalf("got:\n%s\nwant a last scheduled backup line saying no backup since", out.String())
	}
}

func TestDoctorAcceptsABackupFromYesterday(t *testing.T) {
	f := &run.Fake{}
	setupHome(t, f)
	recent := time.Now().Add(-30 * time.Hour).UTC().Format(time.RFC3339)
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "security" {
			return "pw\n", nil
		}
		if len(c.Args) > 0 && c.Args[0] == "snapshots" {
			return `[{"time":"` + recent + `","short_id":"aaa111","paths":["/Users/x"],"tags":["home"]}]`, nil
		}
		return "", nil
	}
	cmd := Root(Deps{Version: "t", Runner: f})
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs([]string{"doctor"})
	if err := cmd.Execute(); err != nil || strings.Contains(out.String(), "no backup since") {
		t.Fatalf("got %v:\n%s\nwant a healthy doctor", err, out.String())
	}
}
