package run

import (
	"context"
	"strings"
	"sync"
)

type Response struct {
	Out string
	Err error
}

// Fake records every command; it is safe for the parallel tasks of a Replace. Its Handler runs
// unlocked, so a test whose handler keeps state guards it.
type Fake struct {
	mu        sync.Mutex
	Calls     []Cmd
	Responses map[string]Response
	Handler   func(Cmd) (string, error)
	Default   Response
}

func (f *Fake) Run(_ context.Context, c Cmd) (string, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, c)
	f.mu.Unlock()
	if f.Handler != nil {
		return f.Handler(c)
	}
	if r, ok := f.Responses[strings.TrimSpace(c.Name+" "+strings.Join(c.Args, " "))]; ok {
		return r.Out, r.Err
	}
	return f.Default.Out, f.Default.Err
}

// CalledWith reports whether any recorded call contains all given tokens in name+args.
func (f *Fake) CalledWith(tokens ...string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.Calls {
		joined := c.Name + " " + strings.Join(c.Args, " ")
		ok := true
		for _, tok := range tokens {
			if !strings.Contains(joined, tok) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
