package run

import (
	"context"
	"strings"
)

type Response struct {
	Out string
	Err error
}

type Fake struct {
	Calls     []Cmd
	Responses map[string]Response
	Handler   func(Cmd) (string, error)
	Default   Response
}

func (f *Fake) Run(_ context.Context, c Cmd) (string, error) {
	f.Calls = append(f.Calls, c)
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
