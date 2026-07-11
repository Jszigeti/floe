package digest

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDueWhenNeverSent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if !Due(time.Now()) {
		t.Fatal("want due when no stamp exists")
	}
}

func TestDueHomeErrorIsNotDue(t *testing.T) {
	t.Setenv("HOME", "")
	if Due(time.Now()) {
		t.Fatal("want not due when HOME is unresolvable")
	}
}

func TestMarkSentHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	if err := MarkSent(time.Now()); err == nil {
		t.Fatal("want error when HOME is unresolvable")
	}
}

func TestDueWithMalformedStamp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p := filepath.Join(home, ".local/state/floe", "last_digest")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("not-a-timestamp"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Due(time.Now()) {
		t.Fatal("want due when the stamp file is malformed")
	}
}

func TestMarkNoticeMkdirAllFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Block .local/state with a regular file so MkdirAll(.../floe) fails.
	if err := os.MkdirAll(filepath.Join(home, ".local"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".local/state"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MarkSent(time.Now()); err == nil {
		t.Fatal("want error when the state dir cannot be created")
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
