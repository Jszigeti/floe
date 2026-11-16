package restic

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/Jszigeti/floe/internal/run"
)

func newTestClient(f *run.Fake) *Client {
	return &Client{Bin: "restic", Repo: "/repo", Pass: "pw", R: f}
}

func TestClientInjectsRepoAndPasswordEnv(t *testing.T) {
	f := &run.Fake{}
	_ = newTestClient(f).Init(context.Background())
	env := f.Calls[0].Env
	if !slices.Contains(env, "RESTIC_REPOSITORY=/repo") || !slices.Contains(env, "RESTIC_PASSWORD=pw") {
		t.Fatalf("env = %v", env)
	}
}

func TestIsInitialized(t *testing.T) {
	ok := &run.Fake{}
	initialized, err := newTestClient(ok).IsInitialized(context.Background())
	if err != nil || !initialized {
		t.Fatalf("initialized = %v, err = %v, want true, nil", initialized, err)
	}

	_, exit10Err := run.ExecRunner{}.Run(context.Background(), run.Cmd{Name: "sh", Args: []string{"-c", "exit 10"}})
	if exit10Err == nil {
		t.Fatal("want exit 10 to produce an error")
	}
	missing := &run.Fake{Default: run.Response{Err: exit10Err}}
	initialized, err = newTestClient(missing).IsInitialized(context.Background())
	if err != nil || initialized {
		t.Fatalf("initialized = %v, err = %v, want false, nil", initialized, err)
	}

	broken := &run.Fake{Default: run.Response{Err: errors.New("connection refused")}}
	initialized, err = newTestClient(broken).IsInitialized(context.Background())
	if err == nil || initialized {
		t.Fatalf("initialized = %v, err = %v, want false, non-nil", initialized, err)
	}
}

func TestBackupBuildsArgs(t *testing.T) {
	f := &run.Fake{}
	err := newTestClient(f).Backup(context.Background(),
		[]string{"/Users/x"}, []string{"node_modules", "/Users/x/.Trash"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"backup", "--quiet", "/Users/x",
		"--exclude", "node_modules", "--exclude", "/Users/x/.Trash"}
	if !slices.Equal(f.Calls[0].Args, want) {
		t.Fatalf("args = %v", f.Calls[0].Args)
	}
}

func TestBackupClassifiesExitCode3(t *testing.T) {
	_, exit3Err := run.ExecRunner{}.Run(context.Background(), run.Cmd{Name: "sh", Args: []string{"-c", "exit 3"}})
	if exit3Err == nil {
		t.Fatal("want exit 3 to produce an error")
	}

	incomplete := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "backup" {
			return "", exit3Err
		}
		return "", nil
	}}
	err := newTestClient(incomplete).Backup(context.Background(), []string{"/Users/x"}, nil, nil)
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("err = %v, want ErrIncomplete", err)
	}

	plain := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "backup" {
			return "", errors.New("boom")
		}
		return "", nil
	}}
	err = newTestClient(plain).Backup(context.Background(), []string{"/Users/x"}, nil, nil)
	if errors.Is(err, ErrIncomplete) {
		t.Fatal("plain error must not match ErrIncomplete")
	}
}

func TestBackupCollectsUnreadablePaths(t *testing.T) {
	// The "err"+"or:" split keeps the literal script source (which ExecRunner's
	// error also embeds via argv) from matching unreadablePathRe itself — only
	// the actual stderr produced by running the script (shell concatenates the
	// adjacent quotes back into "error: ") should match.
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

	incomplete := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "backup" {
			return "", exit3Err
		}
		return "", nil
	}}
	err := newTestClient(incomplete).Backup(context.Background(), []string{"/Users/x"}, nil, nil)

	var ie *IncompleteError
	if !errors.As(err, &ie) {
		t.Fatalf("err = %v, want *IncompleteError", err)
	}
	want := []string{
		"/Users/x/Library/Mail/1",
		"/Users/x/Library/Mail/2",
		"/Users/x/Library/Mail/3",
		"/Users/x/Library/Mail/4",
		"/Users/x/Library/Mail/5",
	}
	if !slices.Equal(ie.Unreadable, want) {
		t.Fatalf("Unreadable = %v, want %v", ie.Unreadable, want)
	}
	if !errors.Is(err, ErrIncomplete) {
		t.Fatal("want errors.Is(err, ErrIncomplete) true")
	}
	if got := ie.Error(); got != "backup incomplete: 5 files could not be read" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestBackupStreamsProgressWhenSet(t *testing.T) {
	f := &run.Fake{}
	c := newTestClient(f)
	var buf bytes.Buffer
	c.Progress = &buf
	if err := c.Backup(context.Background(), []string{"/Users/x"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(f.Calls[0].Args, "--quiet") {
		t.Fatalf("args = %v, want no --quiet when streaming progress", f.Calls[0].Args)
	}
	if !slices.Contains(f.Calls[0].Env, "RESTIC_PROGRESS_FPS=1") {
		t.Fatalf("env = %v, want RESTIC_PROGRESS_FPS=1", f.Calls[0].Env)
	}
	if f.Calls[0].Stdout != &buf {
		t.Fatal("want Stdout wired to Progress writer")
	}
}

func TestBackupPassesTags(t *testing.T) {
	f := &run.Fake{}
	if err := newTestClient(f).Backup(context.Background(), []string{"/Users/x"}, nil, []string{"home"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"backup", "--quiet", "--tag", "home", "/Users/x"}
	if !slices.Equal(f.Calls[0].Args, want) {
		t.Fatalf("args = %q, want %q", f.Calls[0].Args, want)
	}
}

func TestTagUntagged(t *testing.T) {
	f := &run.Fake{}
	if err := newTestClient(f).TagUntagged(context.Background(), "home"); err != nil {
		t.Fatal(err)
	}
	want := []string{"tag", "--tag", "", "--add", "home"}
	if !slices.Equal(f.Calls[0].Args, want) {
		t.Fatalf("args = %q, want %q", f.Calls[0].Args, want)
	}
}

func TestTagBuildsArgs(t *testing.T) {
	f := &run.Fake{}
	id, err := newTestClient(f).Tag(context.Background(), "abc", []string{"replaced"}, []string{"home"})
	if err != nil || id != "abc" {
		t.Fatalf("Tag = %q, %v, want the same id when nothing changed", id, err)
	}
	want := []string{"tag", "--json", "--add", "replaced", "--remove", "home", "abc"}
	if !slices.Equal(f.Calls[0].Args, want) {
		t.Fatalf("args = %q, want %q", f.Calls[0].Args, want)
	}
}

// restic writes a tagged snapshot under a new id and removes the old one.
func TestTagReturnsTheNewID(t *testing.T) {
	f := &run.Fake{Default: run.Response{Out: `{"message_type":"changed","old_snapshot_id":"36dd84e1aa","new_snapshot_id":"7859b457bb"}
{"message_type":"summary","changed_snapshots":1}
`}}
	id, err := newTestClient(f).Tag(context.Background(), "36dd84e1", []string{"replaced"}, nil)
	if err != nil || id != "7859b457bb" {
		t.Fatalf("Tag = %q, %v, want the new id", id, err)
	}
	f = &run.Fake{Default: run.Response{Err: errors.New("locked")}}
	if _, err := newTestClient(f).Tag(context.Background(), "36dd84e1", []string{"replaced"}, nil); err == nil {
		t.Fatal("want restic's error")
	}
}
