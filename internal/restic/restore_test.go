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
	if _, err := newTestClient(f).Latest(context.Background(), "replaced"); !errors.Is(err, ErrNoSnapshot) {
		t.Fatalf("err = %v, want ErrNoSnapshot when no snapshot carries the tag", err)
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

// Shapes printed by restic 0.19.1 `restore --json` (a run that restores some files and skips others,
// one that skips everything), plus a status sent before any file is counted.
const restoreOutput = `{"message_type":"status","percent_done":0,"total_files":5,"files_skipped":1,"total_bytes":12,"bytes_skipped":3000000}
{"message_type":"status","percent_done":0.37,"total_files":6,"files_restored":3,"files_skipped":4,"total_bytes":150000012,"bytes_restored":56386884,"bytes_skipped":453000000}
{"message_type":"status","percent_done":0,"total_files":1,"files_skipped":5,"bytes_skipped":453000006}
{"message_type":"status","percent_done":0,"total_files":0}
{"message_type":"summary","total_files":6,"files_restored":6,"files_skipped":4,"total_bytes":150000012,"bytes_restored":150000012,"bytes_skipped":453000000}
`

func restore(f *run.Fake, status func(RestoreStatus)) error {
	return newTestClient(f).RestoreFolder(context.Background(), "abc", "/Users/alice", "/Users/bob", "/tmp/excludes", status)
}

func TestRestoreFolderArgs(t *testing.T) {
	f := streamingFake("", nil)
	if err := restore(f, func(RestoreStatus) {}); err != nil {
		t.Fatal(err)
	}
	want := []string{"restore", "--json", "abc:/Users/alice", "--target", "/Users/bob", "--exclude-file", "/tmp/excludes"}
	if !slices.Equal(f.Calls[0].Args, want) || !slices.Contains(f.Calls[0].Env, "RESTIC_PROGRESS_FPS=1") {
		t.Fatalf("call = %+v", f.Calls[0])
	}
}

func TestRestoreFolderReportsStatus(t *testing.T) {
	var got []RestoreStatus
	if err := restore(streamingFake("not json\n"+restoreOutput, nil), func(s RestoreStatus) { got = append(got, s) }); err != nil {
		t.Fatal(err)
	}
	want := []RestoreStatus{
		{Done: 3000000, Total: 3000012, Skipped: 3000000},
		{Done: 56386884 + 453000000, Total: 150000012 + 453000000, Skipped: 453000000},
		{Done: 453000006, Total: 453000006, Skipped: 453000006},
		{Loading: true},
		{Done: 150000012 + 453000000, Total: 150000012 + 453000000, Skipped: 453000000},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("status = %+v, want %+v", got, want)
	}
}

func TestRestoreFolderCollectsErrors(t *testing.T) {
	script := `printf '{"message_type":"error","error":{"message":"chmod /Users/bob/Docs/b.txt: denied"},"during":"restore","item":"/Docs/b.txt"}\n` +
		`{"message_type":"error","error":{"message":"chmod /Users/bob/Docs/a.txt: denied"},"during":"restore","item":"/Docs/a.txt"}\n` +
		`{"message_type":"error","error":{"message":"lchown /Users/bob/Docs/a.txt: denied"},"during":"restore","item":"/Docs/a.txt"}\n` +
		`{"message_type":"exit_error","code":1,"message":"Fatal: There were 3 errors"}\n' >&2; exit 1`
	f := &run.Fake{Default: run.Response{Err: exitErr(t, script)}}
	err := restore(f, func(RestoreStatus) {})
	var partial *PartialRestoreError
	if !errors.As(err, &partial) || !slices.Equal(partial.Failed, []string{"/Docs/a.txt", "/Docs/b.txt"}) {
		t.Fatalf("err = %v", err)
	}
}

func TestRestoreFolderKeepsFatalErrors(t *testing.T) {
	f := &run.Fake{Default: run.Response{Err: exitErr(t, `echo '{"message_type":"exit_error","code":1,"message":"Fatal: no matching ID found"}' >&2; exit 1`)}}
	err := restore(f, func(RestoreStatus) {})
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
