package restic

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/Jszigeti/floe/internal/run"
)

// exitErr returns the error ExecRunner gives for a command that writes stderr and exits code.
func exitErr(t *testing.T, script string) error {
	t.Helper()
	_, err := run.ExecRunner{}.Run(context.Background(), run.Cmd{Name: "sh", Args: []string{"-c", script}})
	if err == nil {
		t.Fatal("want the script to fail")
	}
	return err
}

func TestOpenReportsAWrongPassphrase(t *testing.T) {
	f := &run.Fake{Default: run.Response{Err: exitErr(t, "exit 12")}}
	if err := newTestClient(f).Open(context.Background()); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("err = %v, want ErrWrongPassphrase", err)
	}
	ok := &run.Fake{}
	if err := newTestClient(ok).Open(context.Background()); err != nil || !ok.CalledWith("cat", "config") {
		t.Fatalf("err = %v, calls = %+v", err, ok.Calls)
	}
}

func TestLatestPicksTheNewestAcrossHosts(t *testing.T) {
	f := &run.Fake{Responses: map[string]run.Response{
		"restic snapshots --json": {Out: `[
 {"time":"2026-07-10T13:00:00Z","short_id":"new","paths":["/Users/x"]},
 {"time":"2026-07-09T13:00:00Z","short_id":"old","paths":["/Users/x"]}
]`},
	}}
	snap, err := newTestClient(f).Latest(context.Background(), "home")
	if err != nil || snap.ShortID != "new" {
		t.Fatalf("Latest = %+v, %v", snap, err)
	}
}

func TestLatestPicksNewestOfTag(t *testing.T) {
	f := &run.Fake{Responses: map[string]run.Response{
		"restic snapshots --json": {Out: `[
 {"time":"2026-07-11T13:00:00Z","short_id":"apps","paths":["/Applications/X.app"],"tags":["apps"]},
 {"time":"2026-07-10T13:00:00Z","short_id":"untagged","paths":["/Users/x"]},
 {"time":"2026-07-09T13:00:00Z","short_id":"home","paths":["/Users/x"],"tags":["home"]}
]`},
	}}
	snap, err := newTestClient(f).Latest(context.Background(), "home")
	if err != nil || snap.ShortID != "untagged" {
		t.Fatalf("Latest(home) = %+v, %v, want the untagged snapshot", snap, err)
	}
	snap, err = newTestClient(f).Latest(context.Background(), "apps")
	if err != nil || snap.ShortID != "apps" || !slices.Equal(snap.Tags, []string{"apps"}) {
		t.Fatalf("Latest(apps) = %+v, %v", snap, err)
	}
	if _, err := newTestClient(f).Latest(context.Background(), "replaced"); err == nil {
		t.Fatal("want an error when no snapshot carries the tag")
	}
}

func TestLatestFailsWithoutSnapshot(t *testing.T) {
	f := &run.Fake{Responses: map[string]run.Response{"restic snapshots --json": {Out: "[]"}}}
	if _, err := newTestClient(f).Latest(context.Background(), "home"); err == nil {
		t.Fatal("want an error for a backup with no snapshot")
	}
}

func TestRestoreSizeParsesStats(t *testing.T) {
	f := &run.Fake{Responses: map[string]run.Response{
		"restic stats abc --mode restore-size --json": {Out: `{"total_size":42}`},
	}}
	size, err := newTestClient(f).RestoreSize(context.Background(), "abc")
	if err != nil || size != 42 {
		t.Fatalf("RestoreSize = %d, %v", size, err)
	}
}

func TestRestoreFolderArgs(t *testing.T) {
	f := &run.Fake{}
	if err := newTestClient(f).RestoreFolder(context.Background(), "abc", "/Users/alice", "/Users/bob", []string{"/.config/floe"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"restore", "abc:/Users/alice", "--target", "/Users/bob", "--exclude", "/.config/floe"}
	if !slices.Equal(f.Calls[0].Args, want) || !slices.Contains(f.Calls[0].Env, "RESTIC_PROGRESS_FPS=1") {
		t.Fatalf("call = %+v", f.Calls[0])
	}
}

func TestRestoreFolderListsSkippedPaths(t *testing.T) {
	script := `printf 'ignoring error for /Docs/b.txt: denied\nignoring error for /Docs/a.txt: denied\nignoring error for /Docs/a.txt: denied\nFatal: There were 3 errors\n' >&2; exit 1`
	f := &run.Fake{Default: run.Response{Err: exitErr(t, script)}}
	err := newTestClient(f).RestoreFolder(context.Background(), "abc", "/Users/alice", "/Users/bob", nil)
	var partial *PartialRestoreError
	if !errors.As(err, &partial) || !slices.Equal(partial.Failed, []string{"/Docs/a.txt", "/Docs/b.txt"}) {
		t.Fatalf("err = %v", err)
	}
}

func TestRestoreFolderKeepsFatalErrors(t *testing.T) {
	f := &run.Fake{Default: run.Response{Err: exitErr(t, "echo 'Fatal: no matching ID found' >&2; exit 1")}}
	err := newTestClient(f).RestoreFolder(context.Background(), "abc", "/Users/alice", "/Users/bob", nil)
	var partial *PartialRestoreError
	if err == nil || errors.As(err, &partial) {
		t.Fatalf("err = %v, want a plain failure", err)
	}
}

func TestDumpPrintsOneFile(t *testing.T) {
	f := &run.Fake{Responses: map[string]run.Response{
		"restic dump abc /Users/alice/.config/floe/config.toml": {Out: "hour = 9\n"},
	}}
	out, err := newTestClient(f).Dump(context.Background(), "abc", "/Users/alice/.config/floe/config.toml")
	if err != nil || out != "hour = 9\n" {
		t.Fatalf("Dump = %q, %v", out, err)
	}
}
