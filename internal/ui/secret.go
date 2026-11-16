package ui

import (
	"os"
	"os/signal"
	"strings"
	"syscall"
	"unsafe"
)

// ReadSecret reads one line from f, without echoing it when f is a terminal. The echo comes
// back on return, error included, and before Ctrl-C, Ctrl-\, a hangup or a kill ends floe. It reads byte by byte, so
// nothing past the line is consumed.
func ReadSecret(f *os.File) (string, error) {
	var saved syscall.Termios
	if ioctl(f, syscall.TIOCGETA, unsafe.Pointer(&saved)) != nil {
		return readLine(f)
	}
	quiet := saved
	quiet.Lflag &^= syscall.ECHO
	if err := ioctl(f, syscall.TIOCSETA, unsafe.Pointer(&quiet)); err != nil {
		return "", err
	}
	restore := func() { _ = ioctl(f, syscall.TIOCSETA, unsafe.Pointer(&saved)) }
	defer restoreOnSignal(restore)()
	defer restore()
	return readLine(f)
}

// restoreOnSignal runs restore before a signal ends the process, then lets it end, so the
// shell never inherits a terminal that hides what the user types.
func restoreOnSignal(restore func()) (stop func()) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	done := make(chan struct{})
	go func() {
		select {
		case s := <-sigs:
			restore()
			signal.Stop(sigs)
			_ = syscall.Kill(os.Getpid(), s.(syscall.Signal)) // cannot fail on floe's own pid
		case <-done:
		}
	}()
	return func() {
		signal.Stop(sigs)
		close(done)
	}
}

func readLine(f *os.File) (string, error) {
	var line []byte
	b := make([]byte, 1)
	for {
		if _, err := f.Read(b); err != nil {
			return "", err
		}
		if b[0] == '\n' {
			return strings.TrimSuffix(string(line), "\r"), nil
		}
		line = append(line, b[0])
	}
}
