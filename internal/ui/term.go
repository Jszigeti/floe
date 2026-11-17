// Package ui draws floe's progress on the terminal: a live block on a TTY, plain lines elsewhere.
package ui

import (
	"os"
	"syscall"
	"unsafe"
)

// winsize mirrors struct winsize from <sys/ttycom.h>, which package syscall does not export on darwin.
type winsize struct {
	Row, Col, Xpixel, Ypixel uint16
}

// IsTerminal reports whether f is a terminal, false for /dev/null, pipes and files.
func IsTerminal(f *os.File) bool {
	var t syscall.Termios
	return ioctl(f, syscall.TIOCGETA, unsafe.Pointer(&t)) == nil
}

// Width is f's column count, 80 when f is not a terminal or reports no size.
func Width(f *os.File) int {
	var ws winsize
	if ioctl(f, syscall.TIOCGWINSZ, unsafe.Pointer(&ws)) != nil || ws.Col == 0 {
		return 80
	}
	return int(ws.Col)
}

// ioctl runs through SyscallConn, since f.Fd() would switch f to blocking mode for good.
func ioctl(f *os.File, req uintptr, arg unsafe.Pointer) error {
	conn, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var errno syscall.Errno
	if err := conn.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	}); err != nil {
		return err
	}
	if errno != 0 {
		return errno
	}
	return nil
}
