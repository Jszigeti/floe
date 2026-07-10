package restic

import (
	"context"
	"testing"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/run"
)

const snapshotsJSON = `[
 {"time":"2026-07-09T13:00:00.0+02:00","short_id":"aaa111","paths":["/Users/x"]},
 {"time":"2026-07-10T13:00:00.0+02:00","short_id":"bbb222","paths":["/Users/x"]}
]`

func TestSnapshotsParsesJSON(t *testing.T) {
	f := &run.Fake{Responses: map[string]run.Response{
		"restic snapshots --json": {Out: snapshotsJSON},
	}}
	snaps, err := newTestClient(f).Snapshots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 2 || snaps[1].ShortID != "bbb222" || snaps[1].Time.Day() != 10 {
		t.Fatalf("snaps = %+v", snaps)
	}
}

func TestForgetBuildsRetentionArgs(t *testing.T) {
	f := &run.Fake{}
	newTestClient(f).Forget(context.Background(), config.Retention{Daily: 7, Weekly: 4, Monthly: 6})
	if !f.CalledWith("forget", "--prune", "--keep-daily 7", "--keep-weekly 4", "--keep-monthly 6") {
		t.Fatalf("args = %v", f.Calls[0].Args)
	}
}

func TestRestoreArgs(t *testing.T) {
	f := &run.Fake{}
	newTestClient(f).Restore(context.Background(), "bbb222", "/tmp/out", "/Users/x/doc.txt")
	if !f.CalledWith("restore", "bbb222", "--target /tmp/out", "--include /Users/x/doc.txt") {
		t.Fatalf("args = %v", f.Calls[0].Args)
	}
	f2 := &run.Fake{}
	newTestClient(f2).Restore(context.Background(), "latest", "/tmp/out", "")
	if f2.CalledWith("--include") {
		t.Fatal("must omit --include when empty")
	}
}

func TestRepoSizeParsesStats(t *testing.T) {
	f := &run.Fake{Responses: map[string]run.Response{
		"restic stats --mode raw-data --json": {Out: `{"total_size":123456789,"total_file_count":10}`},
	}}
	n, err := newTestClient(f).RepoSize(context.Background())
	if err != nil || n != 123456789 {
		t.Fatalf("n = %d, err = %v", n, err)
	}
}
