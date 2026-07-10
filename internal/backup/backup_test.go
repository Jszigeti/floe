package backup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/restic"
	"github.com/Jszigeti/floe/internal/run"
)

type notified struct{ titles, msgs []string }

func (n *notified) fn(title, msg string) { n.titles = append(n.titles, title); n.msgs = append(n.msgs, msg) }

func deps(t *testing.T, f *run.Fake) (Deps, *notified) {
	t.Setenv("HOME", t.TempDir())
	n := &notified{}
	cfg, _ := config.Default()
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
