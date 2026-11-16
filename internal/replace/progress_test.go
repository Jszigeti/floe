package replace

import (
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/restic"
)

func TestStallAfterThreeMinutes(t *testing.T) {
	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	s := &Stall{After: 3 * time.Minute, Now: func() time.Time { return now }}
	at := func(d time.Duration) { now = now.Add(d) }

	if got := s.Seen(10); got != 0 {
		t.Fatalf("first sight = %v, want 0", got)
	}
	at(2 * time.Minute)
	if got := s.Seen(10); got != 0 {
		t.Fatalf("still for 2m = %v, want 0 before 3m", got)
	}
	at(time.Minute)
	if got := s.Seen(10); got != 3*time.Minute {
		t.Fatalf("still for 3m = %v, want 3m", got)
	}
	at(time.Minute)
	if got := s.Seen(20); got != 0 {
		t.Fatalf("progress = %v, want 0", got)
	}
	at(2 * time.Minute)
	if got := s.Seen(20); got != 0 {
		t.Fatalf("2m after progress = %v, want 0", got)
	}
}

func TestStallNeverCountsLoading(t *testing.T) {
	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	s := &Stall{After: 3 * time.Minute, Now: func() time.Time { return now }}

	s.SeenRestore(restic.RestoreStatus{Done: 5, Total: 9})
	now = now.Add(10 * time.Minute)
	if got := s.SeenRestore(restic.RestoreStatus{Loading: true}); got != 0 {
		t.Fatalf("loading for 10m = %v, want 0", got)
	}
	now = now.Add(time.Minute)
	if got := s.SeenRestore(restic.RestoreStatus{Done: 5, Total: 9}); got != 0 {
		t.Fatalf("first status after loading = %v, want 0", got)
	}
	now = now.Add(3 * time.Minute)
	if got := s.SeenRestore(restic.RestoreStatus{Done: 5, Total: 9}); got != 3*time.Minute {
		t.Fatalf("still for 3m after loading = %v, want 3m", got)
	}
}
