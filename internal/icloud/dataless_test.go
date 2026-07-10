package icloud

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/run"
)

const sfDatalessTest = 0x40000000

func TestDatalessFilesFindsFlaggedFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "b"), nil, 0o644)
	fn := func(p string) (uint32, error) {
		if filepath.Base(p) == "b" {
			return sfDatalessTest, nil
		}
		return 0, nil
	}
	got, err := DatalessFiles(dir, fn)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || filepath.Base(got[0]) != "b" {
		t.Fatalf("got = %v", got)
	}
}

func TestMaterializeDownloadsAndPolls(t *testing.T) {
	f := &run.Fake{}
	var polls atomic.Int32
	fn := func(p string) (uint32, error) {
		if polls.Add(1) > 2 {
			return 0, nil // materialized after two polls
		}
		return sfDatalessTest, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := Materialize(ctx, f, fn, []string{"/repo/pack1"}); err != nil {
		t.Fatal(err)
	}
	if !f.CalledWith("brctl", "download", "/repo/pack1") {
		t.Fatalf("calls = %+v", f.Calls)
	}
}

func TestMaterializeHonorsContext(t *testing.T) {
	f := &run.Fake{}
	fn := func(p string) (uint32, error) { return sfDatalessTest, nil } // never materializes
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := Materialize(ctx, f, fn, []string{"/repo/pack1"}); err == nil {
		t.Fatal("want context error")
	}
}
