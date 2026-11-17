package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/apps"
	"github.com/Jszigeti/floe/internal/replace"
	"github.com/Jszigeti/floe/internal/restic"
	"github.com/Jszigeti/floe/internal/run"
	"github.com/Jszigeti/floe/internal/ui"
)

// testScreen is a screen on a clock the test moves, reading what the test sets: the run's
// progress, and the bytes in Homebrew's download folder.
func testScreen(live bool) (*screen, *replace.Progress, func(time.Duration), *int64) {
	var seen replace.Progress
	var downloaded int64
	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	s := newScreen(nil, func() replace.Progress { return seen }, func() int64 { return downloaded }, live, "on power", func() time.Time { return now })
	return s, &seen, func(d time.Duration) { now = now.Add(d) }, &downloaded
}

func TestScreenRestoreLine(t *testing.T) {
	s, seen, wait, _ := testScreen(true)
	steps := []struct {
		wait time.Duration
		st   restic.RestoreStatus
		want string
	}{
		{0, restic.RestoreStatus{}, "Restoring files  loading the backup…"},
		{4 * time.Minute, restic.RestoreStatus{Loading: true}, "Restoring files  loading the backup…"},
		{0, restic.RestoreStatus{Done: 2e9, Total: 8e9}, "Restoring files  2.0 of 8.0 GB  ###........."},
		{10 * time.Minute, restic.RestoreStatus{Done: 4e9, Total: 8e9}, "Restoring files  4.0 of 8.0 GB  ######......  20m left"},
		{3 * time.Minute, restic.RestoreStatus{Done: 4e9, Total: 8e9}, "Restoring files  4.0 of 8.0 GB  ######......  stalled for 3m: iCloud is not sending the backup, check your connection and Apple account"},
		{47 * time.Minute, restic.RestoreStatus{Done: 4.4e9, Total: 8e9}, "Restoring files  4.4 of 8.0 GB  ######......  1h 30m left"},
	}
	for _, step := range steps {
		wait(step.wait)
		seen.Restore = step.st
		if got := s.lines()[0]; got != step.want {
			t.Errorf("after %v with %+v:\n got %q\nwant %q", step.wait, step.st, got, step.want)
		}
	}
	if got, want := s.lines()[2], "  1h 4m elapsed, on power, sleep blocked"; got != want {
		t.Errorf("clock line = %q, want %q", got, want)
	}
	seen.RestoreEnded = true
	if got, want := s.lines()[0], "  Restore done, 4.4 GB"; got != want {
		t.Errorf("ended restore line = %q, want %q", got, want)
	}
}

func TestScreenAppsLine(t *testing.T) {
	s, seen, wait, _ := testScreen(true)
	if got, want := s.lines()[1], "Installing apps"; got != want {
		t.Errorf("before any report = %q, want %q", got, want)
	}
	seen.Apps = apps.Progress{Total: 31, Current: "Homebrew", Tool: "the Homebrew installer"}
	seen.AppsLines = 1
	if got, want := s.lines()[1], "Installing apps  Homebrew"; got != want {
		t.Errorf("Homebrew line = %q, want %q", got, want)
	}
	seen.Apps = apps.Progress{Done: 11, At: 12, Total: 31, Current: "Google Chrome", Tool: "brew"}
	seen.AppsLines = 2
	if got, want := s.lines()[1], "Installing apps  12 of 31  Google Chrome"; got != want {
		t.Errorf("entry line = %q, want %q", got, want)
	}
	wait(10 * time.Minute)
	if got, want := s.lines()[1], "Installing apps  12 of 31  stalled for 10m: brew is waiting"; got != want {
		t.Errorf("stalled line = %q, want %q", got, want)
	}
	seen.AppsLines = 3
	if got, want := s.lines()[1], "Installing apps  12 of 31  Google Chrome"; got != want {
		t.Errorf("a new line of output must end the stall: got %q, want %q", got, want)
	}
	seen.Apps, seen.AppsEnded = apps.Progress{Done: 31, Total: 31}, true
	if got, want := s.lines()[1], "  Apps done, 31 of 31"; got != want {
		t.Errorf("ended apps line = %q, want %q", got, want)
	}
}

// Off a terminal every change is a new line, so nothing that moves on its own is printed, and
// the restore moves by whole percents.
func TestScreenPlainLeavesOutTheClocks(t *testing.T) {
	s, seen, wait, _ := testScreen(false)
	seen.Restore = restic.RestoreStatus{Done: 2e9, Total: 8e9}
	s.lines()
	wait(10 * time.Minute)
	seen.Restore.Done = 4e9
	lines := s.lines()
	if want := []string{"Restoring files  50%", "  on power, sleep blocked"}; lines[0] != want[0] || lines[2] != want[1] {
		t.Errorf("plain lines = %q, want lines 0 and 2 %q", lines, want)
	}
	seen.Restore.Done = 4.07e9
	if got, want := s.lines()[0], "Restoring files  50%"; got != want {
		t.Errorf("0.07 GB more is no new percent, so no new line: got %q, want %q", got, want)
	}
	seen.Restore.Done = 4.08e9
	if got, want := s.lines()[0], "Restoring files  51%"; got != want {
		t.Errorf("next percent = %q, want %q", got, want)
	}
}

// On a resume restic skips what is in place at once: the time left counts from the first
// restored byte, at the pace of restored bytes only.
func TestScreenTimeLeftCountsRestoredBytesOnly(t *testing.T) {
	s, seen, wait, _ := testScreen(true)
	for _, step := range []struct {
		wait time.Duration
		st   restic.RestoreStatus
		want string
	}{
		{0, restic.RestoreStatus{Total: 10e9}, "Restoring files  0.0 of 10.0 GB  ............"},
		{time.Minute, restic.RestoreStatus{Done: 6e9, Total: 10e9, Skipped: 6e9}, "Restoring files  6.0 of 10.0 GB  #######....."},
		{time.Minute, restic.RestoreStatus{Done: 6.5e9, Total: 10e9, Skipped: 6e9}, "Restoring files  6.5 of 10.0 GB  #######....."},
		{10 * time.Minute, restic.RestoreStatus{Done: 7.5e9, Total: 10e9, Skipped: 6e9}, "Restoring files  7.5 of 10.0 GB  #########...  25m left"},
	} {
		wait(step.wait)
		seen.Restore = step.st
		if got := s.lines()[0]; got != step.want {
			t.Errorf("with %+v:\n got %q\nwant %q", step.st, got, step.want)
		}
	}
}

// restic's first pass walks the backup's tree: the total grows, nothing is restored yet.
func TestScreenTreeWalkIsNoStall(t *testing.T) {
	s, seen, wait, _ := testScreen(true)
	for total := int64(1e9); total <= 5e9; total += 1e9 {
		seen.Restore = restic.RestoreStatus{Total: total}
		if got := s.lines()[0]; strings.Contains(got, "stalled") {
			t.Fatalf("a growing total is no stall: got %q, want no \"stalled\"", got)
		}
		wait(2 * time.Minute)
	}
}

// The preferences and the app bundles come after the home and report no status: no stall then.
func TestScreenRestoreStepsAfterTheHome(t *testing.T) {
	s, seen, wait, _ := testScreen(true)
	seen.Restore = restic.RestoreStatus{Done: 8e9, Total: 8e9}
	s.lines()
	seen.HomeDone = true
	wait(10 * time.Minute)
	if got, want := s.lines()[0], "Restoring app settings"; got != want {
		t.Errorf("preferences line = %q, want %q", got, want)
	}
	seen.Bundle, seen.Bundles = 3, 7
	wait(10 * time.Minute)
	if got, want := s.lines()[0], "Restoring apps from the backup  3 of 7"; got != want {
		t.Errorf("bundles line = %q, want %q", got, want)
	}
	seen.RestoreEnded = true
	if got, want := s.lines()[0], "  Restore done, 8.0 GB"; got != want {
		t.Errorf("ended restore line = %q, want %q", got, want)
	}
}

// brew downloads a cask silently off a terminal: its download folder growing is a sign of life.
func TestScreenAppsDownloadIsNoStall(t *testing.T) {
	s, seen, wait, downloaded := testScreen(true)
	seen.Apps = apps.Progress{Done: 3, At: 4, Total: 9, Current: "Xcode", Tool: "brew"}
	s.lines()
	for range 4 {
		wait(5 * time.Minute)
		*downloaded += 1e8
		if got, want := s.lines()[1], "Installing apps  4 of 9  Xcode"; got != want {
			t.Fatalf("a growing download is no stall: got %q, want %q", got, want)
		}
	}
	wait(10 * time.Minute)
	if got, want := s.lines()[1], "Installing apps  4 of 9  stalled for 10m: brew is waiting"; got != want {
		t.Errorf("neither a line nor a byte for 10m = %q, want %q", got, want)
	}
}

// mas prints nothing while the App Store downloads; its own time limit reports a hang.
func TestScreenMasShowsNoStall(t *testing.T) {
	s, seen, wait, _ := testScreen(true)
	seen.Apps = apps.Progress{Done: 3, At: 4, Total: 9, Current: "497799835", Tool: "mas"}
	s.lines()
	wait(25 * time.Minute)
	if got, want := s.lines()[1], "Installing apps  4 of 9  497799835"; got != want {
		t.Errorf("mas line = %q, want %q", got, want)
	}
}

func TestScreenFollowsUntilStopped(t *testing.T) {
	var out bytes.Buffer
	block := ui.NewBlock(&out, false)
	s := newScreen(block, func() replace.Progress { return replace.Progress{} }, func() int64 { return 0 }, false, "on power", time.Now)
	s.follow(context.Background())()

	if want := "Restoring files  loading the backup…\nInstalling apps\n  on power, sleep blocked\n"; out.String() != want {
		t.Errorf("plain output = %q, want %q", out.String(), want)
	}
}

func TestPausingHandsTheTerminalOver(t *testing.T) {
	var out bytes.Buffer
	block := ui.NewBlock(&out, true)
	block.Set(0, "Restoring files")
	var during []string
	f := &run.Fake{Handler: func(run.Cmd) (string, error) {
		during = append(during, out.String())
		return "", nil
	}}
	r := pausing{Runner: f, block: block}
	_, _ = r.Run(context.Background(), run.Cmd{Name: "brew"})
	_, _ = r.Run(context.Background(), run.Cmd{Name: "/bin/sh", Terminal: true})

	if during[0] != "" {
		t.Errorf("a command off the terminal paused the screen: %q", during[0])
	}
	if !strings.HasSuffix(during[1], "\x1b[?25h") {
		t.Errorf("the screen was not paused, cursor shown, for a terminal command: %q", during[1])
	}
	if len(f.Calls) != 2 {
		t.Errorf("calls = %+v", f.Calls)
	}
}

func TestReadyNamesWhatWasAsked(t *testing.T) {
	for _, c := range []struct {
		store, admin bool
		want         string
	}{
		{false, false, "Disk access ready"},
		{true, false, "Disk access and App Store ready"},
		{false, true, "Disk access and admin password ready"},
		{true, true, "Disk access, App Store and admin password ready"},
	} {
		if got := ready(c.store, c.admin); got != c.want {
			t.Errorf("ready(%v, %v) = %q, want %q", c.store, c.admin, got, c.want)
		}
	}
}
