package ui

import (
	"os"
	"testing"
)

func TestIsTerminalFalseForDevNullAndPipe(t *testing.T) {
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = null.Close() }()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close(); _ = w.Close() }()

	for _, f := range []*os.File{null, r, w} {
		if IsTerminal(f) {
			t.Errorf("IsTerminal(%s) = true, want false", f.Name())
		}
	}
}

func TestWidthDefaultsTo80OffATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close(); _ = w.Close() }()

	if got := Width(w); got != 80 {
		t.Errorf("Width(pipe) = %d, want 80", got)
	}
}
