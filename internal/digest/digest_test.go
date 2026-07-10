package digest

import (
	"testing"
	"time"
)

func TestDueWhenNeverSent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if !Due(time.Now()) {
		t.Fatal("want due when no stamp exists")
	}
}

func TestNotDueRightAfterMark(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now()
	if err := MarkSent(now); err != nil {
		t.Fatal(err)
	}
	if Due(now.Add(24 * time.Hour)) {
		t.Fatal("want not due after 1 day")
	}
	if !Due(now.Add(8 * 24 * time.Hour)) {
		t.Fatal("want due after 8 days")
	}
}
