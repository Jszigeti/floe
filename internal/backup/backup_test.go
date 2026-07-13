package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/restic"
	"github.com/Jszigeti/floe/internal/run"
)

type notified struct{ titles, msgs []string }

func (n *notified) fn(title, msg string) {
	n.titles = append(n.titles, title)
	n.msgs = append(n.msgs, msg)
}

func deps(t *testing.T, f *run.Fake) (Deps, *notified) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	n := &notified{}
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Destination = "/plain/dest" // not iCloud unless a test overrides
	return Deps{
		Cfg:    cfg,
		Client: &restic.Client{Bin: "restic", Repo: "/repo", Pass: "pw", R: f},
		Runner: f,
		Notify: n.fn,
		Now:    time.Now,
		Log:    func(string, ...any) {},
	}, n
}

func TestHappyPathRunsForgetThenBackup(t *testing.T) {
	f := &run.Fake{}
	d, n := deps(t, f)
	if err := Run(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if !f.CalledWith("restic", "backup") || !f.CalledWith("restic", "forget", "--prune") {
		t.Fatalf("calls = %+v", f.Calls)
	}
	forgetIdx, backupIdx := -1, -1
	for i, c := range f.Calls {
		if len(c.Args) > 0 && c.Args[0] == "forget" && forgetIdx == -1 {
			forgetIdx = i
		}
		if len(c.Args) > 0 && c.Args[0] == "backup" && backupIdx == -1 {
			backupIdx = i
		}
	}
	if forgetIdx == -1 || backupIdx == -1 || forgetIdx >= backupIdx {
		t.Fatalf("want forget before backup, calls = %+v", f.Calls)
	}
	if len(n.titles) != 1 || !strings.Contains(n.titles[0], "healthy") {
		t.Fatalf("want only first-run digest, got %v", n.titles)
	}
}

func TestForgetFailureStillRunsBackup(t *testing.T) {
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "forget" {
			return "", errors.New("prune boom")
		}
		return "", nil
	}}
	d, n := deps(t, f)
	if err := Run(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if !f.CalledWith("restic", "backup") {
		t.Fatal("backup must still run after a forget failure")
	}
	found := false
	for _, m := range n.titles {
		if strings.Contains(m, "retention") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a retention failure notification, got %v", n.titles)
	}
}

func TestPreHookFailureAbortsAndNotifies(t *testing.T) {
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if c.Name == "sh" {
			return "", errors.New("hook boom")
		}
		return "", nil
	}}
	d, n := deps(t, f)
	d.Cfg.Hooks.Pre = []string{"exit 1"}
	if err := Run(context.Background(), d); err == nil {
		t.Fatal("want error")
	}
	if f.CalledWith("restic", "backup") {
		t.Fatal("backup must not run after hook failure")
	}
	if len(n.titles) == 0 || !strings.Contains(n.msgs[0], "pre-hook") {
		t.Fatalf("notifications = %v", n.msgs)
	}
}

func TestBackupFailureNotifies(t *testing.T) {
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "backup" {
			return "", errors.New("disk full")
		}
		return "", nil
	}}
	d, n := deps(t, f)
	if err := Run(context.Background(), d); err == nil {
		t.Fatal("want error")
	}
	if len(n.msgs) == 0 || !strings.Contains(n.msgs[0], "disk full") {
		t.Fatalf("notifications = %v", n.msgs)
	}
}

func TestIncompleteBackupWarnsButSucceeds(t *testing.T) {
	_, exit3Err := run.ExecRunner{}.Run(context.Background(), run.Cmd{Name: "sh", Args: []string{"-c", "exit 3"}})
	if exit3Err == nil {
		t.Fatal("want exit 3 to produce an error")
	}
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "backup" {
			return "", exit3Err
		}
		return "", nil
	}}
	d, n := deps(t, f)
	d.Cfg.Hooks.Post = []string{"echo posthook"}
	if err := Run(context.Background(), d); err != nil {
		t.Fatalf("incomplete backup must not fail Run: %v", err)
	}
	found := false
	for _, title := range n.titles {
		if strings.Contains(title, "incomplete") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want an incomplete-backup notification, got %v", n.titles)
	}
	if !f.CalledWith("restic", "forget", "--prune") {
		t.Fatal("want forget to still run")
	}
	if !f.CalledWith("sh", "-c", "echo posthook") {
		t.Fatal("want post-hooks to still run")
	}
}

func TestIncompleteBackupSummarizesAndLogsFullList(t *testing.T) {
	// Same "err"+"or:" split as restic's client_test.go: keeps the script
	// source (embedded verbatim in ExecRunner's error via argv) from itself
	// matching restic.unreadablePathRe.
	script := `
printf 'err''or: open /Users/x/Library/Mail/1: operation not permitted\n' >&2
printf 'err''or: open /Users/x/Library/Mail/2: operation not permitted\n' >&2
printf 'err''or: open /Users/x/Library/Mail/3: operation not permitted\n' >&2
printf 'err''or: openfile for readdirnames failed: open /Users/x/Library/Mail/4: operation not permitted\n' >&2
printf 'err''or: open /Users/x/Library/Mail/5: operation not permitted\n' >&2
exit 3
`
	_, exit3Err := run.ExecRunner{}.Run(context.Background(), run.Cmd{Name: "sh", Args: []string{"-c", script}})
	if exit3Err == nil {
		t.Fatal("want exit 3 to produce an error")
	}
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "backup" {
			return "", exit3Err
		}
		return "", nil
	}}
	d, _ := deps(t, f)
	var logs []string
	d.Log = func(format string, a ...any) { logs = append(logs, fmt.Sprintf(format, a...)) }

	if err := Run(context.Background(), d); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "5 files") {
		t.Fatalf("log missing file count, got %v", logs)
	}
	for i := 1; i <= 3; i++ {
		want := fmt.Sprintf("/Users/x/Library/Mail/%d", i)
		if !strings.Contains(joined, want) {
			t.Fatalf("log missing example %q, got %v", want, logs)
		}
	}
	if strings.Contains(joined, "--exclude") {
		t.Fatalf("log leaked the full restic argv, got %v", logs)
	}
	if strings.Contains(joined, "incomplete: backup incomplete") {
		t.Fatalf("log has the doubled sentinel prefix, got %v", logs)
	}
	if !strings.Contains(joined, "last_incomplete.log") {
		t.Fatalf("log missing state-file pointer, got %v", logs)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".local/state/floe/last_incomplete.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 5 {
		t.Fatalf("state file lines = %v, want 5", lines)
	}
}

func TestIncompleteNoticeThrottledWeekly(t *testing.T) {
	_, exit3Err := run.ExecRunner{}.Run(context.Background(), run.Cmd{Name: "sh", Args: []string{"-c", "exit 3"}})
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "backup" {
			return "", exit3Err
		}
		return "", nil
	}}
	d, n := deps(t, f)
	now := time.Now()
	d.Now = func() time.Time { return now }

	if err := Run(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), d); err != nil {
		t.Fatal(err)
	}

	count := 0
	for _, title := range n.titles {
		if strings.Contains(title, "incomplete") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("want exactly one incomplete notification across both runs, got %d: %v", count, n.titles)
	}
}

func TestDigestHonestWhenIncomplete(t *testing.T) {
	_, exit3Err := run.ExecRunner{}.Run(context.Background(), run.Cmd{Name: "sh", Args: []string{"-c", "exit 3"}})
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "backup" {
			return "", exit3Err
		}
		return "", nil
	}}
	d, n := deps(t, f)
	if err := Run(context.Background(), d); err != nil {
		t.Fatal(err)
	}

	found := false
	for i, title := range n.titles {
		if strings.Contains(title, "running") {
			found = true
			if strings.Contains(n.msgs[i], "healthy") {
				t.Fatalf("digest message should not claim healthy, got %q", n.msgs[i])
			}
		}
		if strings.Contains(title, "healthy") {
			t.Fatalf("digest title should not claim healthy when backup is incomplete, got %v", n.titles)
		}
	}
	if !found {
		t.Fatalf("want a digest notification titled with 'running', got %v", n.titles)
	}
}

func TestRunDumpsManifest(t *testing.T) {
	f := &run.Fake{}
	d, _ := deps(t, f)
	if err := Run(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if !f.CalledWith("brew", "bundle", "dump") {
		t.Fatal("want manifest dump to run brew bundle dump")
	}
}

func TestRunSucceedsWhenManifestToolsFail(t *testing.T) {
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		switch c.Name {
		case "brew", "mas", "crontab":
			return "", errors.New("no such tool")
		}
		return "", nil
	}}
	d, _ := deps(t, f)
	if err := Run(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if !f.CalledWith("restic", "backup") {
		t.Fatal("backup must still run when manifest tools are all missing")
	}
}

func TestSkipsForgetWhenICloudSyncPending(t *testing.T) {
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if c.Name == "brctl" && c.Args[0] == "status" {
			return "up:needs-upload", nil
		}
		if c.Name == "brctl" && c.Args[0] == "quota" {
			return "999999999999 bytes of quota remaining", nil
		}
		return "", nil
	}}
	d, _ := deps(t, f)
	d.Cfg.Destination = "/Users/x/Library/Mobile Documents/com~apple~CloudDocs"
	if err := Run(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if f.CalledWith("restic", "forget") {
		t.Fatal("must not prune while repo sync is pending")
	}
}

func TestSkipsForgetWhenSyncStateUnknown(t *testing.T) {
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if c.Name == "brctl" && c.Args[0] == "status" {
			return "", errors.New("brctl unavailable")
		}
		if c.Name == "brctl" && c.Args[0] == "quota" {
			return "999999999999 bytes of quota remaining", nil
		}
		return "", nil
	}}
	d, _ := deps(t, f)
	d.Cfg.Destination = "/Users/x/Library/Mobile Documents/com~apple~CloudDocs"
	if err := Run(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if f.CalledWith("restic", "forget") {
		t.Fatal("must not prune when sync state is unknown")
	}
}

func TestManifestDumpFailureIsLoggedNotFatal(t *testing.T) {
	f := &run.Fake{}
	d, _ := deps(t, f)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	// Block the manifest dir with a regular file so manifest.Dump's MkdirAll fails.
	if err := os.MkdirAll(filepath.Join(home, ".local/state/floe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".local/state/floe/manifest"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var logs []string
	d.Log = func(format string, a ...any) { logs = append(logs, fmt.Sprintf(format, a...)) }

	if err := Run(context.Background(), d); err != nil {
		t.Fatalf("manifest dump failure must not fail Run: %v", err)
	}
	found := false
	for _, l := range logs {
		if strings.Contains(l, "manifest dump skipped") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a manifest dump skipped log line, got %v", logs)
	}
}

func TestPostHookFailureAbortsAndNotifies(t *testing.T) {
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if c.Name == "sh" {
			return "", errors.New("hook boom")
		}
		return "", nil
	}}
	d, n := deps(t, f)
	d.Cfg.Hooks.Post = []string{"exit 1"}
	if err := Run(context.Background(), d); err == nil {
		t.Fatal("want error")
	}
	found := false
	for _, m := range n.msgs {
		if strings.Contains(m, "post-hook") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a post-hook failure notification, got %v", n.msgs)
	}
}

func TestDigestMessageIncludesRepoSize(t *testing.T) {
	f := &run.Fake{Responses: map[string]run.Response{
		"restic stats --mode raw-data --json": {Out: `{"total_size":5000000000}`},
	}}
	d, n := deps(t, f)
	if err := Run(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range n.msgs {
		if strings.Contains(m, "GB") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want digest message to include repo size in GB, got %v", n.msgs)
	}
}

func TestDigestStampAndIncompleteNoticeWriteFailuresAreLogged(t *testing.T) {
	_, exit3Err := run.ExecRunner{}.Run(context.Background(), run.Cmd{Name: "sh", Args: []string{"-c", "exit 3"}})
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "backup" {
			return "", exit3Err
		}
		return "", nil
	}}
	d, _ := deps(t, f)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	// Block the whole state dir with a regular file so every digest stamp write fails.
	if err := os.MkdirAll(filepath.Join(home, ".local/state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".local/state/floe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var logs []string
	d.Log = func(format string, a ...any) { logs = append(logs, fmt.Sprintf(format, a...)) }

	if err := Run(context.Background(), d); err != nil {
		t.Fatalf("stamp write failures must not fail Run: %v", err)
	}
	wantIncomplete, wantDigest := false, false
	for _, l := range logs {
		if strings.Contains(l, "incomplete-notice stamp write failed") {
			wantIncomplete = true
		}
	}
	// The digest stamp write only happens if digest.Due fires; the incomplete
	// backup's message overrides the healthy one, but the stamp is still written.
	for _, l := range logs {
		if strings.Contains(l, "digest stamp write failed") {
			wantDigest = true
		}
	}
	if !wantIncomplete {
		t.Fatalf("want an incomplete-notice stamp write failure log, got %v", logs)
	}
	if !wantDigest {
		t.Fatalf("want a digest stamp write failure log, got %v", logs)
	}
}

func TestQuotaLowNotifies(t *testing.T) {
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if c.Name == "brctl" && c.Args[0] == "quota" {
			return "1000000 bytes of quota remaining", nil
		}
		return "", nil
	}}
	d, n := deps(t, f)
	d.Cfg.Destination = "/Users/x/Library/Mobile Documents/com~apple~CloudDocs"
	if err := Run(context.Background(), d); err != nil {
		t.Fatal(err) // low quota warns but does not block
	}
	found := false
	for _, m := range n.titles {
		if strings.Contains(m, "quota") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want quota notification, got %v", n.titles)
	}
}
