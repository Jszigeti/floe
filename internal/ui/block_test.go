package ui

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// recorder keeps every Write call apart, so a test can tell one frame from another.
type recorder struct {
	mu     sync.Mutex
	writes []string
}

func (r *recorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes = append(r.writes, string(p))
	return len(p), nil
}

func (r *recorder) all() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.writes, "")
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.writes...)
}

func fixedNow() time.Time { return time.Unix(0, 0) }

func TestBlockPlainPrintsChangesOnly(t *testing.T) {
	var out bytes.Buffer
	b := NewBlock(&out, false)
	b.now = fixedNow
	b.Start(context.Background())
	b.Set(0, "Restoring files")
	b.Set(0, "Restoring files")
	b.Set(1, "Installing apps")
	b.Set(0, "Restoring files  1 of 2 GB")
	b.Done("Found the backup", 4*time.Second)
	b.Pause()()
	b.Stop()

	want := "Restoring files\nInstalling apps\nRestoring files  1 of 2 GB\n" +
		"✓ Found the backup" + strings.Repeat(" ", 34) + "4s\n"
	if got := out.String(); got != want {
		t.Errorf("plain output:\n%q\nwant\n%q", got, want)
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Error("plain output carries an escape sequence")
	}
}

func TestBlockLiveFrameIsOneWrite(t *testing.T) {
	rec := &recorder{}
	b := NewBlock(rec, true)
	b.now = fixedNow
	b.Set(0, "Restoring files")
	b.Set(1, "Installing apps")
	b.Set(2, "  1m elapsed")
	b.Start(context.Background())
	b.Stop()

	writes := rec.snapshot()
	if len(writes) == 0 {
		t.Fatal("no frame written")
	}
	for i, w := range writes {
		for _, line := range []string{"⠋ Restoring files", "⠋ Installing apps", "\x1b[2K  1m elapsed"} {
			if !strings.Contains(w, line) {
				t.Errorf("write %d lacks %q: %q", i, line, w)
			}
		}
	}
	if last := writes[len(writes)-1]; !strings.HasPrefix(last, "\x1b[3A") {
		t.Errorf("final frame does not go back up over the 3 live lines: %q", last)
	}
}

func TestBlockDoneLandsAboveTheLiveLines(t *testing.T) {
	rec := &recorder{}
	b := NewBlock(rec, true)
	b.now = fixedNow
	b.Set(0, "Restoring files")
	b.Start(context.Background())
	b.Done("Found the backup", time.Minute+12*time.Second)
	b.Stop()

	writes := rec.snapshot()
	frame := writes[1]
	done := strings.Index(frame, "✓ Found the backup")
	live := strings.Index(frame, "⠋ Restoring files")
	if !strings.HasPrefix(frame, "\x1b[1A") || done < 0 || live < done {
		t.Errorf("Done frame = %q, want the done line drawn over the old live line, live line below", frame)
	}
	if !strings.Contains(frame, "1m 12s\n") {
		t.Errorf("Done frame lacks the duration: %q", frame)
	}
}

func TestBlockCutsToWidth(t *testing.T) {
	rec := &recorder{}
	b := NewBlock(rec, true)
	b.now = fixedNow
	b.width = func() int { return 12 }
	b.Set(0, "Restoring files 12.4 of 21.4 GB")
	b.Stop()

	lines := strings.Split(strings.TrimSuffix(rec.all(), "\x1b[?25h"), "\n")
	got := strings.TrimPrefix(lines[0], "\r\x1b[2K")
	if got != "⠋ Restorin" {
		t.Errorf("cut line = %q, want %q", got, "⠋ Restorin")
	}
	if n := utf8.RuneCountInString(got); n != 10 {
		t.Errorf("cut line is %d columns, want width minus 2 = 10", n)
	}
}

func TestBlockStopShowsCursor(t *testing.T) {
	rec := &recorder{}
	b := NewBlock(rec, true)
	b.now = fixedNow
	b.Set(0, "Restoring files")
	b.Start(context.Background())
	b.Stop()
	b.Stop()

	out := rec.all()
	if !strings.HasPrefix(out, "\x1b[?25l") {
		t.Errorf("Start does not hide the cursor: %q", out)
	}
	if !strings.HasSuffix(out, "\x1b[?25h") || strings.Count(out, "\x1b[?25h") != 1 {
		t.Errorf("Stop does not show the cursor exactly once: %q", out)
	}
}

// A signal ends floe before Stop runs, so the handler shows the cursor while the block is live.
func TestBlockSignalShowsCursor(t *testing.T) {
	var cleanup func()
	removed, writesAtRemoval := 0, 0
	on := onSignal
	rec := &recorder{}
	onSignal = func(f func()) func() {
		cleanup = f
		return func() {
			removed++
			writesAtRemoval = len(rec.snapshot())
		}
	}
	t.Cleanup(func() { onSignal = on })
	plain := NewBlock(&bytes.Buffer{}, false)
	plain.now = fixedNow
	plain.Start(context.Background())
	if cleanup != nil {
		t.Fatal("a plain block hides no cursor: nothing to show on a signal")
	}

	b := NewBlock(rec, true)

	b.now = fixedNow
	b.Set(0, "Restoring files")
	b.Start(context.Background())
	if cleanup == nil {
		t.Fatal("a live block must show the cursor on a signal")
	}
	cleanup()
	if out := rec.all(); !strings.HasSuffix(out, "\x1b[?25h") {
		t.Errorf("the signal cleanup does not show the cursor: %q", out)
	}
	b.Stop()
	if removed != 1 {
		t.Errorf("Stop must drop the signal cleanup once, dropped %d times", removed)
	}
	if writes := rec.snapshot(); writesAtRemoval != len(writes) || !strings.HasSuffix(writes[len(writes)-1], "\x1b[?25h") {
		t.Errorf("Stop must show the cursor before it drops the signal cleanup: %q", writes)
	}
}

func TestBlockContextCancelShowsCursor(t *testing.T) {
	rec := &recorder{}
	b := NewBlock(rec, true)
	b.now = fixedNow
	b.Set(0, "Restoring files")
	ctx, cancel := context.WithCancel(context.Background())
	b.Start(ctx)
	cancel()

	select {
	case <-b.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the redraw loop outlived its context")
	}
	if out := rec.all(); !strings.HasSuffix(out, "\x1b[?25h") {
		t.Errorf("context cancel does not show the cursor: %q", out)
	}
	b.Stop()
}

func TestBlockRedrawsOnTick(t *testing.T) {
	rec := &recorder{}
	now := time.Unix(0, 0)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	b := NewBlock(rec, true)
	b.now = clock
	b.Set(0, "Restoring files")
	b.Start(context.Background())
	defer b.Stop()

	mu.Lock()
	now = now.Add(redraw)
	mu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(rec.all(), "⠙ Restoring files") {
		if time.Now().After(deadline) {
			t.Fatalf("no redraw with the next spinner frame: %q", rec.all())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBlockPauseClearsAndResumes(t *testing.T) {
	rec := &recorder{}
	b := NewBlock(rec, true)
	b.now = fixedNow
	b.Set(0, "Restoring files")
	b.Set(1, "Installing apps")
	b.Start(context.Background())

	resume := b.Pause()
	cleared := rec.snapshot()
	erase := cleared[len(cleared)-1]
	if erase != "\r\x1b[2K\x1b[1A\x1b[2K\x1b[1A\x1b[2K\x1b[?25h" {
		t.Errorf("Pause erase = %q", erase)
	}
	b.Done("Asked for the admin password", 0)
	b.Set(0, "Restoring files  1 of 2 GB")
	if got := len(rec.snapshot()); got != len(cleared) {
		t.Errorf("Pause still draws: %d writes, want %d", got, len(cleared))
	}

	resume()
	resume()
	b.Stop()
	writes := rec.snapshot()
	back := writes[len(cleared)]
	if !strings.HasPrefix(back, "\x1b[?25l\r\x1b[2K✓ Asked for the admin password\n") ||
		!strings.Contains(back, "⠋ Restoring files  1 of 2 GB") {
		t.Errorf("resume frame = %q", back)
	}
	if len(writes) != len(cleared)+2 {
		t.Errorf("resume twice drew %d frames, want 1 then the final one", len(writes)-len(cleared))
	}
}

// A line set to empty leaves the screen, so a summary can follow the block without it.
func TestBlockEmptyLineIsNotDrawn(t *testing.T) {
	rec := &recorder{}
	b := NewBlock(rec, true)
	b.now = fixedNow
	b.Set(0, "Restoring files")
	b.Set(1, "Installing apps")
	b.Start(context.Background())
	b.Set(0, "")
	b.Set(1, "")
	b.Stop()

	writes := rec.snapshot()
	if last := writes[len(writes)-1]; last != "\x1b[2A\x1b[J\x1b[?25h" {
		t.Errorf("final frame = %q, want back up over both lines, erased, cursor shown", last)
	}

	var out bytes.Buffer
	plain := NewBlock(&out, false)
	plain.now = fixedNow
	plain.Set(0, "Restoring files")
	plain.Set(0, "")
	if out.String() != "Restoring files\n" {
		t.Errorf("plain output = %q, want no empty line", out.String())
	}
}

func TestDuration(t *testing.T) {
	cases := map[time.Duration]string{
		4 * time.Second:                 "4s",
		time.Minute + 12*time.Second:    "1m 12s",
		41*time.Minute + 30*time.Second: "41m",
		2*time.Hour + 5*time.Minute + 3: "2h 5m",
		400 * time.Millisecond:          "0s",
		9*time.Minute + 59*time.Second:  "9m 59s",
		10 * time.Minute:                "10m",
		time.Hour:                       "1h 0m",
	}
	for d, want := range cases {
		if got := Duration(d); got != want {
			t.Errorf("Duration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestDoneLineAlignsItsDuration(t *testing.T) {
	if got, want := DoneLine("Restored 21.4 GB of files and settings", 41*time.Minute), "✓ Restored 21.4 GB of files and settings            41m"; got != want {
		t.Errorf("DoneLine = %q, want %q", got, want)
	}
	if got := DoneLine("Asked", 0); got != "✓ Asked" {
		t.Errorf("DoneLine without a duration = %q", got)
	}
}
