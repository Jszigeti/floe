package ui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	redraw     = 500 * time.Millisecond
	hideCursor = "\x1b[?25l"
	showCursor = "\x1b[?25h"
	clearLine  = "\r\x1b[2K"
	// doneColumn aligns the durations of Done lines, as in the spec's screen.
	doneColumn = 49
)

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Block owns the bottom lines of the screen: finished steps scroll up, live lines redraw in place.
// Off a terminal it prints one plain line per change and never an escape sequence.
type Block struct {
	w     io.Writer
	live  bool
	now   func() time.Time
	width func() int

	mu      sync.Mutex
	lines   []string // live lines, or the last printed text of each in plain mode
	pending []string // Done lines not drawn yet
	drawn   int      // live lines currently on screen
	paused  bool
	started bool
	ended   bool

	halt     sync.Once
	stop     chan struct{}
	finished chan struct{}
}

// NewBlock writes to w; live false prints plain lines. now drives the spinner frame.
func NewBlock(w io.Writer, live bool, now func() time.Time) *Block {
	width := func() int { return 80 }
	if f, ok := w.(*os.File); ok {
		width = func() int { return Width(f) }
	}
	return &Block{w: w, live: live, now: now, width: width, stop: make(chan struct{}), finished: make(chan struct{})}
}

// Done prints a finished step, with its duration when d is not zero, above the live lines.
func (b *Block) Done(line string, d time.Duration) {
	text := DoneLine(line, d)
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.live {
		b.write(text + "\n")
		return
	}
	b.pending = append(b.pending, text)
	if !b.paused {
		b.write(b.frame())
	}
}

// Set replaces live line i. A line starting with a space carries no spinner.
func (b *Block) Set(i int, line string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if i >= len(b.lines) {
		b.lines = append(b.lines, make([]string, i+1-len(b.lines))...)
	}
	if b.live || b.lines[i] == line {
		b.lines[i] = line
		return
	}
	b.lines[i] = line
	b.write(line + "\n")
}

// Pause erases the live lines and stops drawing while a child process owns the terminal.
// The returned function resumes drawing; calling it again does nothing.
func (b *Block) Pause() func() {
	if !b.live {
		return func() {}
	}
	b.mu.Lock()
	b.paused = true
	b.write(clearLine + strings.Repeat("\x1b[1A\x1b[2K", b.drawn) + showCursor)
	b.drawn = 0
	b.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.paused = false
			if b.started && !b.ended {
				b.write(hideCursor + b.frame())
			}
		})
	}
}

// Start hides the cursor and redraws every 0.5 s until Stop or ctx is done.
func (b *Block) Start(ctx context.Context) {
	if !b.live {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.started {
		return
	}
	b.started = true
	b.write(hideCursor + b.frame())
	go b.loop(ctx)
}

// Stop draws the final frame and shows the cursor again.
func (b *Block) Stop() {
	if !b.live {
		return
	}
	b.mu.Lock()
	started := b.started
	b.mu.Unlock()
	if !started {
		b.end()
		return
	}
	b.halt.Do(func() { close(b.stop) })
	<-b.finished
}

func (b *Block) loop(ctx context.Context) {
	defer close(b.finished)
	tick := time.NewTicker(redraw)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			b.end()
			return
		case <-b.stop:
			b.end()
			return
		case <-tick.C:
			b.mu.Lock()
			if !b.paused {
				b.write(b.frame())
			}
			b.mu.Unlock()
		}
	}
}

// end leaves the last frame in the scrollback; later Done lines print below it.
func (b *Block) end() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ended {
		return
	}
	b.ended = true
	b.write(b.frame() + showCursor)
	b.lines = nil
	b.drawn = 0
}

// frame goes back up over the drawn live lines, prints pending Done lines, then the live lines.
func (b *Block) frame() string {
	var s strings.Builder
	if b.drawn > 0 {
		fmt.Fprintf(&s, "\x1b[%dA", b.drawn)
	}
	room := b.width() - 2
	for _, line := range b.pending {
		s.WriteString(clearLine + cut(line, room) + "\n")
	}
	b.pending = nil
	spin := spinner[b.now().UnixNano()/int64(redraw)%int64(len(spinner))]
	for _, line := range b.lines {
		if line != "" && !strings.HasPrefix(line, " ") {
			line = spin + " " + line
		}
		s.WriteString(clearLine + cut(line, room) + "\n")
	}
	b.drawn = len(b.lines)
	return s.String()
}

// write ignores errors: a progress line that cannot be shown must never stop the run.
func (b *Block) write(s string) {
	_, _ = io.WriteString(b.w, s)
}

// cut keeps the first n runes: every symbol floe draws is one column wide.
func cut(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:max(n, 0)])
}

// DoneLine is a finished step as the screen shows it, its duration aligned when d is not zero.
func DoneLine(line string, d time.Duration) string {
	if d <= 0 {
		return "✓ " + line
	}
	return fmt.Sprintf("✓ %-*s %s", doneColumn, line, Duration(d))
}

// Duration formats a duration the way the screen shows it: 4s, 1m 12s, 41m, 2h 5m.
func Duration(d time.Duration) string {
	d = d.Truncate(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < 10*time.Minute:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
}
