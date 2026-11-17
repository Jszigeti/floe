package apps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Jszigeti/floe/internal/askpass"
	"github.com/Jszigeti/floe/internal/run"
)

// TestMain lets this test binary stand in for floe when the askpass script runs it.
func TestMain(m *testing.M) {
	if socket := os.Getenv(askpass.SocketEnv); socket != "" {
		if askpass.Answer(socket, os.Stdout) != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// sudoFake answers sudo -A as sudo does: it runs the askpass program from the environment and
// accepts the line it prints when it is want. Every call it sees is recorded.
func sudoFake(t *testing.T, want string) *run.Fake {
	t.Helper()
	return &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if !isSudoA(c) {
			return "", nil
		}
		out, err := askHelper(t, c)
		if err != nil {
			return "", err
		}
		if out != want+"\n" {
			return "", errors.New("sudo: 3 incorrect password attempts")
		}
		return "", nil
	}}
}

func isSudoA(c run.Cmd) bool {
	return filepath.Base(c.Name) == "sudo" && slices.Contains(c.Args, "-A")
}

// askHelper runs the askpass program of c once, as sudo does for each password prompt.
func askHelper(t *testing.T, c run.Cmd) (string, error) {
	t.Helper()
	var program string
	for _, e := range c.Env {
		if p, ok := strings.CutPrefix(e, "SUDO_ASKPASS="); ok {
			program = p
		}
	}
	if info, err := os.Stat(filepath.Dir(program)); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("the askpass folder must be private: %v, %v", info, err)
	}
	return run.ExecRunner{}.Run(context.Background(), run.Cmd{Name: program, Args: []string{"Password:"}})
}

func callArgs(f *run.Fake) []string {
	calls := make([]string, len(f.Calls))
	for i, c := range f.Calls {
		calls[i] = callLine(c)
	}
	return calls
}

func TestStartAdminServesThePasswordThroughAPrivateFolder(t *testing.T) {
	f := sudoFake(t, "pass word")

	a, err := StartAdmin(context.Background(), f, "pass word")
	if err != nil {
		t.Fatal(err)
	}
	env := a.Env()
	if len(env) != 1 || env[0] != "SUDO_ASKPASS="+filepath.Join(a.dir, "askpass") {
		t.Fatalf("Env = %q", env)
	}
	if _, err := os.Stat(filepath.Join(a.dir, "socket")); err != nil {
		t.Fatalf("the socket must sit in the private folder: %v", err)
	}
	if !slices.Equal(callArgs(f), []string{"sudo -k", "sudo -A -v"}) || !slices.Equal(f.Calls[1].Env, env) {
		t.Fatalf("calls = %q, env = %q", callArgs(f), f.Calls[1].Env)
	}

	a.Close()

	if _, err := os.Stat(a.dir); !os.IsNotExist(err) {
		t.Fatalf("Close must remove the private folder: %v", err)
	}
	if !slices.Equal(callArgs(f), []string{"sudo -k", "sudo -A -v", "sudo -k"}) {
		t.Fatalf("Close must drop the admin rights: %q", callArgs(f))
	}
	if strings.Trim(string(a.password), "\x00") != "" {
		t.Fatal("Close must clear floe's own copy of the password")
	}
}

func TestStartAdminRefusesAWrongPassword(t *testing.T) {
	f := sudoFake(t, "right")

	a, err := StartAdmin(context.Background(), f, "wrong")
	if !errors.Is(err, ErrRefused) || a != nil {
		t.Fatalf("StartAdmin = %v, %v", a, err)
	}
	calls := callArgs(f)
	if first := slices.Index(calls, "sudo -A -v"); first < 1 || calls[first-1] != "sudo -k" {
		t.Fatalf("an earlier timestamp must be dropped before the password is checked: %q", calls)
	}
	if !slices.Equal(calls, []string{"sudo -k", "sudo -A -v", "sudo -k"}) {
		t.Fatalf("a refused password must close everything: %q", calls)
	}
	program, _ := strings.CutPrefix(f.Calls[1].Env[0], "SUDO_ASKPASS=")
	if _, err := os.Stat(filepath.Dir(program)); !os.IsNotExist(err) {
		t.Fatalf("a refused password must leave no private folder: %v", err)
	}
}

func TestStartAdminAnswersOneHelperUntilThePasswordIsAccepted(t *testing.T) {
	var asks []string
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if !isSudoA(c) {
			return "", nil
		}
		for range 2 {
			out, err := askHelper(t, c)
			asks = append(asks, fmt.Sprintf("%q %v", out, err != nil))
		}
		return "", errors.New("Sorry, try again.\nsudo: 1 incorrect password attempt")
	}}

	if _, err := StartAdmin(context.Background(), f, "typo"); !errors.Is(err, ErrRefused) {
		t.Fatalf("StartAdmin = %v", err)
	}
	if !slices.Equal(asks, []string{`"typo\n" false`, `"" true`}) {
		t.Fatalf("a second prompt while the password is checked must get nothing, so one typo is one failed login: %q", asks)
	}
}

func TestStartAdminServesEveryHelperOnceThePasswordIsAccepted(t *testing.T) {
	f := sudoFake(t, "pw")
	a, err := StartAdmin(context.Background(), f, "pw")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	for range 2 {
		if out, err := askHelper(t, f.Calls[1]); err != nil || out != "pw\n" {
			t.Fatalf("helper = %q, %v", out, err)
		}
	}
}

func TestStartAdminTellsANonAdminAccountFromAWrongPassword(t *testing.T) {
	for _, stderr := range []string{
		"jonas is not in the sudoers file.",
		"Sorry, user jonas is not allowed to run sudo on mac.",
		"Sorry, user jonas may not run sudo on mac.",
	} {
		f := &run.Fake{Default: run.Response{Err: errExit, Stderr: stderr}}
		if _, err := StartAdmin(context.Background(), f, "pw"); !errors.Is(err, ErrNotAdmin) || errors.Is(err, ErrRefused) {
			t.Errorf("%s: StartAdmin = %v, want ErrNotAdmin", stderr, err)
		}
	}
}

func TestStartAdminWrapsAnyOtherFailure(t *testing.T) {
	f := &run.Fake{Default: run.Response{Err: &run.Error{Name: sudo, Args: []string{"-A", "-v"}, Err: errors.New("signal: killed")}}}

	_, err := StartAdmin(context.Background(), f, "pw")
	if err == nil || errors.Is(err, ErrRefused) || errors.Is(err, ErrNotAdmin) || !strings.Contains(err.Error(), "killed") {
		t.Fatalf("StartAdmin = %v", err)
	}
}

// fakeSignals stands in for floe's signal handler: it keeps the cleanup Admin registers, which
// the test runs as a signal would, and tells when Admin removed it.
func fakeSignals(t *testing.T) (cleanup *func(), removed *atomic.Bool) {
	t.Helper()
	cleanup, removed = new(func()), &atomic.Bool{}
	on := onSignal
	onSignal = func(f func()) func() {
		*cleanup = f
		return func() { removed.Store(true) }
	}
	t.Cleanup(func() { onSignal = on })
	return cleanup, removed
}

func TestAnInterruptClosesTheAdmin(t *testing.T) {
	cleanup, removed := fakeSignals(t)
	f := sudoFake(t, "pw")
	a, err := StartAdmin(context.Background(), f, "pw")
	if err != nil {
		t.Fatal(err)
	}
	if *cleanup == nil || removed.Load() {
		t.Fatal("a running admin must be closed by a signal")
	}

	(*cleanup)()

	// The handler has dropped every cleanup already: the admin has none left to drop.
	if _, err := os.Stat(a.dir); !os.IsNotExist(err) {
		t.Fatalf("the signal must close the admin: %v", err)
	}
	if !slices.Equal(callArgs(f), []string{"sudo -k", "sudo -A -v", "sudo -k"}) {
		t.Fatalf("the signal must drop the admin rights: %q", callArgs(f))
	}
	a.Close()
	if !slices.Equal(callArgs(f), []string{"sudo -k", "sudo -A -v", "sudo -k"}) {
		t.Fatalf("a second Close must do nothing: %q", callArgs(f))
	}
}

// Close drops its cleanup last, so a Ctrl-C during the close waits for it to drop the rights.
func TestCloseDropsTheSignalCleanupLast(t *testing.T) {
	f := sudoFake(t, "pw")
	var a *Admin
	var atRemoval []string
	dirLeft := true
	on := onSignal
	onSignal = func(func()) func() {
		return func() {
			atRemoval = callArgs(f)
			_, err := os.Stat(a.dir)
			dirLeft = err == nil
		}
	}
	t.Cleanup(func() { onSignal = on })
	a, err := StartAdmin(context.Background(), f, "pw")
	if err != nil {
		t.Fatal(err)
	}

	a.Close()

	if atRemoval == nil {
		t.Fatal("Close must drop the signal cleanup")
	}
	if dirLeft || !slices.Equal(atRemoval, []string{"sudo -k", "sudo -A -v", "sudo -k"}) {
		t.Fatalf("the cleanup was dropped before the close ended: folder left %v, calls %q", dirLeft, atRemoval)
	}
}

// A signal right after StartAdmin registers its cleanup closes the admin while StartAdmin goes
// on: -race checks they share nothing unguarded.
func TestASignalDuringStartAdmin(t *testing.T) {
	closed := make(chan struct{})
	on := onSignal
	onSignal = func(cleanup func()) func() {
		go func() {
			cleanup()
			close(closed)
		}()
		return func() {}
	}
	t.Cleanup(func() { onSignal = on })

	a, err := StartAdmin(context.Background(), &run.Fake{}, "pw")
	<-closed

	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.dir); !os.IsNotExist(err) {
		t.Fatalf("the signal must close the admin: %v", err)
	}
	a.Close()
}

func TestRefreshRevalidatesThroughAskpass(t *testing.T) {
	f := sudoFake(t, "pw")
	a, err := StartAdmin(context.Background(), f, "pw")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	if err := a.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(callArgs(f), []string{"sudo -k", "sudo -A -v", "sudo -A -v"}) || !slices.Equal(f.Calls[2].Env, a.Env()) {
		t.Fatalf("calls = %q", callArgs(f))
	}
}

func TestNoAdminHasNoEnvAndClosesQuietly(t *testing.T) {
	var a *Admin
	if a.Env() != nil {
		t.Fatal("no admin, no askpass")
	}
	a.Close()
}

// callLine is a call as typed in a shell, tool base name first.
func callLine(c run.Cmd) string {
	return filepath.Base(c.Name) + " " + strings.Join(c.Args, " ")
}
