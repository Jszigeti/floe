package askpass

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// serveOnce listens on a socket in a short temporary folder, Unix socket paths being capped at 104 bytes.
func serveOnce(t *testing.T, answer string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ap")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "s")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		_, _ = conn.Write([]byte(answer))
		_ = conn.Close()
	}()
	return socket
}

func TestAnswerPrintsWhatTheSocketServesThenANewline(t *testing.T) {
	socket := serveOnce(t, "pass word")
	var out bytes.Buffer

	if err := Answer(socket, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "pass word\n" {
		t.Fatalf("out = %q", out.String())
	}
}

func TestAnswerPrintsNothingWhenTheSocketServesNothing(t *testing.T) {
	socket := serveOnce(t, "")
	var out bytes.Buffer

	if err := Answer(socket, &out); err == nil || out.Len() != 0 {
		t.Fatalf("err = %v, out = %q", err, out.String())
	}
}

func TestAnswerFailsWithoutAServer(t *testing.T) {
	var out bytes.Buffer
	if err := Answer(filepath.Join(t.TempDir(), "none"), &out); err == nil || out.Len() != 0 {
		t.Fatalf("err = %v, out = %q", err, out.String())
	}
}
