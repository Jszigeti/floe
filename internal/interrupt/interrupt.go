// Package interrupt is floe's handler for the signals that end it: it runs what must be undone
// first, then raises the signal once, so floe still ends as it meant to. Every cleanup goes
// through it, the password prompt's echo included, so a signal floe started with ignored stays
// ignored everywhere.
package interrupt

import (
	"maps"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
)

// ending is read at start, as Notify clears what Ignored reports. SIGTERM always stays, since Go
// never keeps it ignored, so Notify never gets an empty list, which would mean every signal.
var ending = endingSignals(signal.Ignored)

// endingSignals leaves out the signals floe started with ignored, SIGHUP under nohup: raised
// again, they would not end floe, which would go on with everything closed.
func endingSignals(ignored func(os.Signal) bool) []os.Signal {
	return slices.DeleteFunc([]os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}, ignored)
}

// watch delivers the signals that end floe, until stop.
var watch = func() (<-chan os.Signal, func()) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, ending...)
	return sigs, func() { signal.Stop(sigs) }
}

// raise sends s to floe again, which ends it once nothing catches it.
var raise = func(s os.Signal) {
	_ = syscall.Kill(os.Getpid(), s.(syscall.Signal)) // floe's own pid with a valid signal: kill(2) cannot fail
}

var (
	mu       sync.Mutex
	cleanups = map[int]func(){}
	next     int
	stop     func() // ends the current watch, nil when nothing is registered
)

// OnSignal runs cleanup when SIGINT, SIGTERM, SIGHUP or SIGQUIT comes, before the signal ends
// floe, until the returned remove. Cleanups run side by side, so a write stuck on a terminal
// paused with Ctrl-S holds back no other; the signal waits for all of them. remove may be called
// from one.
func OnSignal(cleanup func()) (remove func()) {
	mu.Lock()
	defer mu.Unlock()
	id := next
	next++
	cleanups[id] = cleanup
	if stop == nil {
		start()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			mu.Lock()
			defer mu.Unlock()
			delete(cleanups, id)
			if len(cleanups) == 0 && stop != nil {
				stop()
				stop = nil
			}
		})
	}
}

// start watches for one signal; mu is held.
func start() {
	sigs, unwatch := watch()
	done := make(chan struct{})
	stop = func() {
		unwatch()
		close(done)
	}
	go func() {
		select {
		case s := <-sigs:
			mu.Lock()
			run := slices.Collect(maps.Values(cleanups))
			cleanups, stop = map[int]func(){}, nil
			mu.Unlock()
			var wg sync.WaitGroup
			wg.Add(len(run))
			for _, cleanup := range run {
				go func() {
					defer wg.Done()
					cleanup()
				}()
			}
			wg.Wait()
			unwatch()
			raise(s)
		case <-done:
		}
	}()
}
