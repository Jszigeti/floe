package ui

import (
	"bytes"
	"io"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// openPty opens a pseudo-terminal pair, the way a terminal app gives floe its tty.
func openPty(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	var name [128]byte
	for _, req := range []uintptr{syscall.TIOCPTYGRANT, syscall.TIOCPTYUNLK} {
		if err := ioctl(m, req, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := ioctl(m, syscall.TIOCPTYGNAME, unsafe.Pointer(&name)); err != nil {
		t.Fatal(err)
	}
	s, err := os.OpenFile(string(name[:bytes.IndexByte(name[:], 0)]), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(); _ = m.Close() })
	return m, s
}

func echoOn(t *testing.T, f *os.File) bool {
	t.Helper()
	var tio syscall.Termios
	if err := ioctl(f, syscall.TIOCGETA, unsafe.Pointer(&tio)); err != nil {
		t.Fatal(err)
	}
	return tio.Lflag&syscall.ECHO != 0
}

// waitEchoOff waits until ReadSecret turned the echo off, so what the test types is never echoed.
func waitEchoOff(t *testing.T, f *os.File) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for echoOn(t, f) {
		if time.Now().After(deadline) {
			t.Fatal("the echo never went off")
		}
		time.Sleep(time.Millisecond)
	}
}

type secretResult struct {
	s   string
	err error
}

func readSecretAsync(f *os.File) chan secretResult {
	got := make(chan secretResult, 1)
	go func() {
		s, err := ReadSecret(f)
		got <- secretResult{s, err}
	}()
	return got
}

func TestReadSecretHidesTheLineThenRestoresEcho(t *testing.T) {
	master, slave := openPty(t)
	if !echoOn(t, slave) {
		t.Fatal("a new terminal echoes")
	}
	got := readSecretAsync(slave)
	waitEchoOff(t, slave)
	if _, err := master.WriteString("pass word\n"); err != nil {
		t.Fatal(err)
	}

	res := <-got
	if res.err != nil || res.s != "pass word" {
		t.Fatalf("ReadSecret = %q, %v", res.s, res.err)
	}
	if !echoOn(t, slave) {
		t.Fatal("the echo must come back after the line")
	}
}

func TestReadSecretRestoresEchoOnError(t *testing.T) {
	master, slave := openPty(t)
	got := readSecretAsync(slave)
	waitEchoOff(t, slave)
	_ = master.Close()

	if res := <-got; res.err == nil {
		t.Fatalf("a closed terminal must be an error, got %q", res.s)
	}
	if !echoOn(t, slave) {
		t.Fatal("the echo must come back after an error")
	}
}

func TestReadSecretReadsOneLineOffATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if _, err := w.WriteString("pw\r\nnext\n"); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()

	if s, err := ReadSecret(r); err != nil || s != "pw" {
		t.Fatalf("ReadSecret = %q, %v", s, err)
	}
	if rest, _ := io.ReadAll(r); string(rest) != "next\n" {
		t.Fatalf("ReadSecret must stop at the line's end, left %q", rest)
	}
	if s, err := ReadSecret(r); err == nil {
		t.Fatalf("no line left must be an error, got %q", s)
	}
}

// floe's signal handler turns the echo back on before a signal ends floe, and only while the
// prompt is open; a signal floe started with ignored, SIGHUP under nohup, it leaves alone.
func TestReadSecretRestoresEchoOnSignal(t *testing.T) {
	cleanups, removed := make(chan func(), 1), make(chan struct{}, 1)
	on := onSignal
	onSignal = func(f func()) func() {
		cleanups <- f
		return func() { removed <- struct{}{} }
	}
	t.Cleanup(func() { onSignal = on })
	master, slave := openPty(t)
	got := readSecretAsync(slave)
	waitEchoOff(t, slave)

	var cleanup func()
	select {
	case cleanup = <-cleanups:
	case <-time.After(2 * time.Second):
		t.Fatal("the prompt must register with floe's signal handler")
	}
	cleanup()
	if !echoOn(t, slave) {
		t.Fatal("a signal must turn the echo back on")
	}
	if _, err := master.WriteString("pw\n"); err != nil {
		t.Fatal(err)
	}
	if res := <-got; res.err != nil || res.s != "pw" {
		t.Fatalf("ReadSecret = %q, %v", res.s, res.err)
	}
	select {
	case <-removed:
	default:
		t.Fatal("the prompt must leave floe's signal handler on return")
	}
}
