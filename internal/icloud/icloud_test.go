package icloud

import (
	"context"
	"testing"

	"github.com/Jszigeti/floe/internal/run"
)

func TestIsICloudPath(t *testing.T) {
	if !IsICloudPath("/Users/x/Library/Mobile Documents/com~apple~CloudDocs") {
		t.Fatal("want true for iCloud Drive path")
	}
	if IsICloudPath("/Volumes/Backup") {
		t.Fatal("want false for external disk")
	}
}

func TestQuotaRemainingParsesBrctl(t *testing.T) {
	f := &run.Fake{Responses: map[string]run.Response{
		"brctl quota": {Out: "54022418096 bytes of quota remaining in personal account\n"},
	}}
	n, err := QuotaRemaining(context.Background(), f)
	if err != nil || n != 54022418096 {
		t.Fatalf("n = %d, err = %v", n, err)
	}
}

func TestSyncPending(t *testing.T) {
	pending := &run.Fake{Default: run.Response{Out: "r:8386 ld{synced-all} up:needs-upload uv:1"}}
	if got, _ := SyncPending(context.Background(), pending); !got {
		t.Fatal("want pending when needs-upload present")
	}
	clean := &run.Fake{Default: run.Response{Out: "r:8386 ld{synced-all} uv:1"}}
	if got, _ := SyncPending(context.Background(), clean); got {
		t.Fatal("want not pending")
	}
}

func TestOptimizeStorageEnabled(t *testing.T) {
	on := &run.Fake{Responses: map[string]run.Response{
		"defaults read com.apple.bird optimize-storage": {Out: "1\n"},
	}}
	if !OptimizeStorageEnabled(context.Background(), on) {
		t.Fatal("want enabled")
	}
	unset := &run.Fake{Default: run.Response{Err: contextErr()}}
	if OptimizeStorageEnabled(context.Background(), unset) {
		t.Fatal("want disabled when key missing")
	}
}

func contextErr() error { return context.DeadlineExceeded }
