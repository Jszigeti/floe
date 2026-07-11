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

	missingRepoErr := errors.New(`Fatal: unable to open config file: stat config: no such file or directory
Is there a repository at the following location?`)
	missing := &run.Fake{Default: run.Response{Err: missingRepoErr}}
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
		[]string{"/Users/x"}, []string{"node_modules", "/Users/x/.Trash"})
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
	err := newTestClient(incomplete).Backup(context.Background(), []string{"/Users/x"}, nil)
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("err = %v, want ErrIncomplete", err)
	}

	plain := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if len(c.Args) > 0 && c.Args[0] == "backup" {
			return "", errors.New("boom")
		}
		return "", nil
	}}
	err = newTestClient(plain).Backup(context.Background(), []string{"/Users/x"}, nil)
	if errors.Is(err, ErrIncomplete) {
		t.Fatal("plain error must not match ErrIncomplete")
	}
}

func TestBackupStreamsProgressWhenSet(t *testing.T) {
	f := &run.Fake{}
	c := newTestClient(f)
	var buf bytes.Buffer
	c.Progress = &buf
	if err := c.Backup(context.Background(), []string{"/Users/x"}, nil); err != nil {
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
