package ui

import (
	"errors"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// ReadSecret reads one line from f, without echoing it when f is a terminal. The echo comes
// back on return, error included, and before floe's signal handler lets Ctrl-C, Ctrl-\, a hangup
// or a kill end floe. It reads byte by byte, so nothing past the line is consumed.
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
	restore := func() error { return ioctl(f, syscall.TIOCSETA, unsafe.Pointer(&saved)) }
	defer onSignal(func() { _ = restore() })() // floe is ending: there is no one left to tell
	line, err := readLine(f)
	if err = errors.Join(err, restore()); err != nil {
		return "", err
	}
	return line, nil
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
