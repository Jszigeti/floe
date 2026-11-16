package cli

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/Jszigeti/floe/internal/replace"
	"github.com/Jszigeti/floe/internal/restic"
	"github.com/Jszigeti/floe/internal/run"
	"github.com/Jszigeti/floe/internal/ui"
)

// screen keeps the live lines of a run up to date: the restore, the apps, the clock.
type screen struct {
	block *ui.Block
	read  func() replace.Progress
	// downloaded is what Homebrew's download folder holds, which grows while brew downloads silently.
	downloaded func() int64
	live       bool
	power      string
	now        func() time.Time
	start      time.Time

	restoreStall, appsStall, downloadStall replace.Stall
	// The first restored bytes and when they came, the pace the time left is measured from.
	from   int64
	fromAt time.Time
}

func newScreen(block *ui.Block, read func() replace.Progress, downloaded func() int64, live bool, power string, now func() time.Time) *screen {
	return &screen{
		block: block, read: read, downloaded: downloaded, live: live, power: power, now: now, start: now(),
		restoreStall:  replace.Stall{After: 3 * time.Minute, Now: now},
		appsStall:     replace.Stall{After: 10 * time.Minute, Now: now},
		downloadStall: replace.Stall{After: 10 * time.Minute, Now: now},
	}
}

// liveScreen reports whether w is a terminal that can redraw lines in place.
func liveScreen(w io.Writer) bool {
	f, ok := w.(*os.File)
	term := os.Getenv("TERM")
	return ok && ui.IsTerminal(f) && term != "" && term != "dumb"
}

// follow draws the lines now and every half second until the returned stop, which takes them
// off the screen: the summary says what they did.
func (s *screen) follow(ctx context.Context) (stop func()) {
	s.block.Start(ctx)
	done, exited := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(exited)
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			for i, line := range s.lines() {
				s.block.Set(i, line)
			}
			select {
			case <-done:
				return
			case <-tick.C:
			}
		}
	}()
	return func() {
		close(done)
		<-exited
		for i := range 3 {
			s.block.Set(i, "")
		}
		s.block.Stop()
	}
}

// lines are the restore's, the apps' and the clock's. Off a terminal each change prints a line,
// so the elapsed and left times, which move on their own, stay out.
func (s *screen) lines() [3]string {
	seen, now := s.read(), s.now()
	clock := "  " + s.power + ", sleep blocked"
	if s.live {
		clock = fmt.Sprintf("  %s elapsed, %s, sleep blocked", ui.Duration(now.Sub(s.start)), s.power)
	}
	return [3]string{s.restoreLine(seen, now), s.appsLine(seen), clock}
}

func (s *screen) restoreLine(seen replace.Progress, now time.Time) string {
	st := seen.Restore
	switch {
	case seen.RestoreEnded:
		return fmt.Sprintf("  Restore done, %.1f GB", float64(st.Done)/1e9)
	case seen.Bundle > 0:
		return fmt.Sprintf("Restoring apps from the backup  %d of %d", seen.Bundle, seen.Bundles)
	case seen.HomeDone:
		return "Restoring app settings"
	}
	// restic prints nothing while it loads the index, so no status yet is loading too.
	st.Loading = st.Loading || st.Total == 0
	stalled := s.restoreStall.SeenRestore(st)
	if st.Loading {
		return "Restoring files  loading the backup…"
	}
	// Off a terminal each change prints a line: whole percents keep them to a hundred.
	line := fmt.Sprintf("Restoring files  %d%%", st.Done*100/st.Total)
	if s.live {
		line = fmt.Sprintf("Restoring files  %.1f of %.1f GB  %s", float64(st.Done)/1e9, float64(st.Total)/1e9, bar(st.Done, st.Total))
	}
	if stalled > 0 {
		return line + fmt.Sprintf("  stalled for %dm: iCloud is not sending the backup, check your connection and Apple account", int(stalled.Minutes()))
	}
	if left := s.left(st, now); left > 0 && s.live {
		return line + "  " + minutes(left) + " left"
	}
	return line
}

// left is the time the restore still takes at its pace since the first restored bytes, 0 until
// it moved. Skipped bytes, which a resume counts at once, set no pace.
func (s *screen) left(st restic.RestoreStatus, now time.Time) time.Duration {
	restored := st.Done - st.Skipped
	if s.fromAt.IsZero() {
		if restored <= 0 {
			return 0
		}
		s.from, s.fromAt = restored, now
	}
	moved := restored - s.from
	if moved <= 0 {
		return 0
	}
	return time.Duration(float64(now.Sub(s.fromAt)) * float64(st.Total-st.Done) / float64(moved))
}

func (s *screen) appsLine(seen replace.Progress) string {
	p := seen.Apps
	if seen.AppsEnded {
		return fmt.Sprintf("  Apps done, %d of %d", p.Done, p.Total)
	}
	line := "Installing apps"
	if p.At > 0 {
		line += fmt.Sprintf("  %d of %d", p.At, p.Total)
	}
	// A line or a downloaded byte is a sign of life. mas shows neither: its time limit reports a hang.
	stalled := min(s.appsStall.Seen(seen.AppsLines), s.downloadStall.Seen(s.downloaded()))
	if stalled > 0 && p.Tool != "mas" {
		return line + fmt.Sprintf("  stalled for %dm: %s is waiting", int(stalled.Minutes()), p.Tool)
	}
	if p.Current != "" {
		line += "  " + p.Current
	}
	return line
}

// bar is done of total in twelve cells.
func bar(done, total int64) string {
	n := int(min(max(done*12/total, 0), 12))
	return strings.Repeat("#", n) + strings.Repeat(".", 12-n)
}

// minutes is a time left in hours and minutes, at least 1m.
func minutes(d time.Duration) string {
	m := max(int(math.Round(d.Minutes())), 1)
	if m < 60 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh %dm", m/60, m%60)
}

// pausing stops the screen's redraw while a command owns the terminal, so its prompt is never
// drawn over.
type pausing struct {
	run.Runner
	block *ui.Block
}

func (p pausing) Run(ctx context.Context, c run.Cmd) (string, error) {
	if c.Terminal {
		defer p.block.Pause()()
	}
	return p.Runner.Run(ctx, c)
}
