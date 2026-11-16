package apps

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Jszigeti/floe/internal/askpass"
	"github.com/Jszigeti/floe/internal/interrupt"
	"github.com/Jszigeti/floe/internal/run"
)

// ErrRefused is sudo refusing the administrator password.
var ErrRefused = errors.New("the administrator password was refused")

// ErrNotAdmin is sudo refusing the account itself, whatever the password.
var ErrNotAdmin = errors.New("this account may not run sudo")

// sudo is called by its full path, as Homebrew does, so no other sudo on PATH gets the password.
const sudo = "/usr/bin/sudo"

// Admin holds the administrator password for one run and gives it to every sudo that Homebrew
// and mas start, so the user types it once. Homebrew resets sudo's timestamp in each of its
// processes, so keeping the timestamp fresh is not enough.
//
// sudo runs SUDO_ASKPASS, a script in a folder only the user's account can open, which starts
// floe as askpass helper on the socket beside it. The socket path rides in the script, not in
// the environment, because brew drops every variable but a short list, SUDO_ASKPASS included.
type Admin struct {
	r         run.Runner
	dir       string
	listener  net.Listener
	password  []byte
	served    chan struct{}
	validated atomic.Bool
	unwatch   func()
	closing   sync.Once
}

// onSignal has floe's signal handler close an Admin before Ctrl-C or a kill ends floe.
var onSignal = interrupt.OnSignal

// StartAdmin serves password on a private socket, then checks it with sudo. A refused password
// returns ErrRefused, an account that may not use sudo ErrNotAdmin, both with everything closed.
func StartAdmin(ctx context.Context, r run.Runner, password string) (*Admin, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "floe-admin-")
	if err != nil {
		return nil, err
	}
	a := &Admin{r: r, dir: dir, password: []byte(password), served: make(chan struct{})}
	socket := filepath.Join(dir, "socket")
	script := fmt.Sprintf("#!/bin/sh\n%s=%s exec %s\n", askpass.SocketEnv, shellQuote(socket), shellQuote(exe))
	if err := os.WriteFile(filepath.Join(dir, "askpass"), []byte(script), 0o700); err != nil {
		a.Close()
		return nil, err
	}
	if a.listener, err = net.Listen("unix", socket); err != nil {
		a.Close()
		return nil, err
	}
	go a.serve()
	// The handler gets close, not Close: it drops its cleanups itself, and close never reads
	// unwatch, which a signal coming right now would race with.
	a.unwatch = onSignal(a.close)
	// A timestamp left by an earlier sudo would let a mistyped password through unchecked.
	_, _ = r.Run(ctx, run.Cmd{Name: sudo, Args: []string{"-k"}})
	if err := a.Refresh(ctx); err != nil {
		a.Close()
		return nil, validationError(err)
	}
	a.validated.Store(true)
	return a, nil
}

// validationError tells an account sudo refuses from a wrong password, from any other failure.
func validationError(err error) error {
	has := func(phrases ...string) bool {
		return slices.ContainsFunc(phrases, func(p string) bool { return strings.Contains(err.Error(), p) })
	}
	if has("is not in the sudoers file", "is not allowed to run", "may not run") {
		return fmt.Errorf("%w: %w", ErrNotAdmin, err)
	}
	if has("incorrect password attempt", "Sorry, try again") {
		return fmt.Errorf("%w: %w", ErrRefused, err)
	}
	return fmt.Errorf("checking the administrator password: %w", err)
}

// serve hands the password to one askpass helper until sudo accepts it, so a typo costs one
// failed login, not three, then to every helper until Close.
// Any process of the user's account can read it while the install runs: an accepted limit.
func (a *Admin) serve() {
	defer close(a.served)
	answered := false
	for {
		conn, err := a.listener.Accept()
		if err != nil {
			return
		}
		if !answered || a.validated.Load() {
			// A failed write leaves the helper with nothing to print, and sudo then stops asking.
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_, _ = conn.Write(a.password)
			answered = true
		}
		_ = conn.Close() // nothing is left to send on it
	}
}

// Env is what every brew and mas command needs to reach the password, nil without an Admin.
func (a *Admin) Env() []string {
	if a == nil {
		return nil
	}
	return []string{"SUDO_ASKPASS=" + filepath.Join(a.dir, "askpass")}
}

// Refresh renews sudo's timestamp, for mas, which calls sudo without -A.
func (a *Admin) Refresh(ctx context.Context) error {
	_, err := a.r.Run(ctx, run.Cmd{Name: sudo, Args: []string{"-A", "-v"}, Env: a.Env()})
	return err
}

// Close stops serving, removes the private folder, clears floe's own copy of the password and
// drops sudo's timestamp, so the admin rights end with the run. The string the password came in
// stays in memory until collected: Go strings cannot be wiped. Only the first call does anything.
func (a *Admin) Close() {
	if a == nil {
		return
	}
	a.close()
	// Last, so a signal during the close waits for it instead of ending floe with the rights on.
	if a.unwatch != nil {
		a.unwatch()
	}
}

// close ends the admin rights once; a second caller waits for the first to finish.
func (a *Admin) close() {
	a.closing.Do(func() {
		// Best effort on every step: Close runs on the way out, with no one left to report to.
		if a.listener != nil {
			_ = a.listener.Close()
			<-a.served
		}
		_ = os.RemoveAll(a.dir)
		clear(a.password)
		_, _ = a.r.Run(context.Background(), run.Cmd{Name: sudo, Args: []string{"-k"}})
	})
}

// shellQuote wraps s in single quotes for /bin/sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
