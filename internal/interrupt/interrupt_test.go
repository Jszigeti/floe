package interrupt

import (
	"os"
	"slices"
	"sync"
	"syscall"
	"testing"
	"time"
)

// fake replaces the process signals with a channel and records what is raised again.
type fake struct {
	sigs    chan os.Signal
	raised  chan os.Signal
	mu      sync.Mutex
	watches int
	stops   int
}

func fakeSignals(t *testing.T) *fake {
	t.Helper()
	f := &fake{sigs: make(chan os.Signal, 1), raised: make(chan os.Signal, 2)}
	w, r := watch, raise
	watch = func() (<-chan os.Signal, func()) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.watches++
		return f.sigs, func() { f.mu.Lock(); f.stops++; f.mu.Unlock() }
	}
	raise = func(s os.Signal) { f.raised <- s }
	t.Cleanup(func() { watch, raise = w, r })
	return f
}

func (f *fake) counts() (watches, stops int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.watches, f.stops
}

func TestASignalRunsEveryCleanupThenRaisesItOnce(t *testing.T) {
	f := fakeSignals(t)
	var mu sync.Mutex
	var ran []string
	record := func(name string) func() {
		return func() { mu.Lock(); ran = append(ran, name); mu.Unlock() }
	}
	removeAdmin := OnSignal(record("admin"))
	OnSignal(record("cursor"))

	f.sigs <- syscall.SIGINT
	select {
	case s := <-f.raised:
		if s != syscall.SIGINT {
			t.Fatalf("raised %v, want the same signal", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the signal must be raised again so floe still ends")
	}
	mu.Lock()
	defer mu.Unlock()
	if slices.Sort(ran); !slices.Equal(ran, []string{"admin", "cursor"}) {
		t.Fatalf("cleanups ran %q, want each once", ran)
	}
	if watches, stops := f.counts(); watches != 1 || stops != 1 {
		t.Fatalf("one watch for both cleanups, stopped before the raise: %d watches, %d stops", watches, stops)
	}
	removeAdmin() // a cleanup that runs its own removal must not block
	if len(f.raised) != 0 {
		t.Fatal("the signal must be raised once")
	}
}

// A cursor write stuck on a terminal paused with Ctrl-S must not keep the admin rights alive.
func TestAStuckCleanupHoldsBackNoOther(t *testing.T) {
	f := fakeSignals(t)
	resume, adminClosed := make(chan struct{}), make(chan struct{})
	OnSignal(func() { close(adminClosed) })
	OnSignal(func() { <-resume })

	f.sigs <- syscall.SIGINT
	select {
	case <-adminClosed:
	case <-time.After(2 * time.Second):
		t.Fatal("a stuck cleanup held back the others")
	}
	select {
	case <-f.raised:
		t.Fatal("the signal must wait for every cleanup")
	case <-time.After(50 * time.Millisecond):
	}
	close(resume)
	select {
	case <-f.raised:
	case <-time.After(2 * time.Second):
		t.Fatal("the signal must be raised once every cleanup is done")
	}
}

// Under nohup SIGHUP is ignored: catching it would close everything and leave floe running.
func TestEndingSignalsLeaveOutTheIgnoredOnes(t *testing.T) {
	got := endingSignals(func(s os.Signal) bool { return s == syscall.SIGHUP })
	if !slices.Equal(got, []os.Signal{syscall.SIGINT, syscall.SIGTERM}) {
		t.Fatalf("watched %v, want SIGINT and SIGTERM only", got)
	}
	if got := endingSignals(func(os.Signal) bool { return false }); len(got) != 3 {
		t.Fatalf("watched %v, want all three", got)
	}
}

func TestRemovingTheLastCleanupStopsWatching(t *testing.T) {
	f := fakeSignals(t)
	first := OnSignal(func() { t.Error("a removed cleanup ran") })
	second := OnSignal(func() { t.Error("a removed cleanup ran") })
	first()
	first()
	if _, stops := f.counts(); stops != 0 {
		t.Fatal("a cleanup is still registered: keep watching")
	}
	second()
	if watches, stops := f.counts(); watches != 1 || stops != 1 {
		t.Fatalf("%d watches, %d stops, want the watch stopped with the last cleanup", watches, stops)
	}
	OnSignal(func() {})()
	if watches, stops := f.counts(); watches != 2 || stops != 2 {
		t.Fatalf("%d watches, %d stops, want a new watch for a new cleanup", watches, stops)
	}
	if len(f.raised) != 0 {
		t.Fatal("no signal, nothing raised")
	}
}

// A real SIGINT to the test process is safe here: it is caught while a cleanup is registered,
// and the raise that would end the process is the fake's.
func TestARealInterruptRunsTheCleanup(t *testing.T) {
	r := raise
	raised := make(chan os.Signal, 1)
	raise = func(s os.Signal) { raised <- s }
	t.Cleanup(func() { raise = r })
	ran := make(chan struct{})
	defer OnSignal(func() { close(ran) })()

	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("the cleanup did not run on SIGINT")
	}
	if s := <-raised; s != syscall.SIGINT {
		t.Fatalf("raised %v", s)
	}
}
