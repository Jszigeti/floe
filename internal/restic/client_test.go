package restic

import (
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
	newTestClient(f).Init(context.Background())
	env := f.Calls[0].Env
	if !slices.Contains(env, "RESTIC_REPOSITORY=/repo") || !slices.Contains(env, "RESTIC_PASSWORD=pw") {
		t.Fatalf("env = %v", env)
	}
}

func TestIsInitialized(t *testing.T) {
	ok := &run.Fake{}
	if !newTestClient(ok).IsInitialized(context.Background()) {
		t.Fatal("want initialized when cat config succeeds")
	}
	bad := &run.Fake{Default: run.Response{Err: errors.New("no repo")}}
	if newTestClient(bad).IsInitialized(context.Background()) {
		t.Fatal("want not initialized on error")
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
