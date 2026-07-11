package restic

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// bzip2 has no stdlib writer; serve a pre-built tiny bz2 fixture instead.
// Fixture created once with: printf '#!/bin/sh\necho fake-restic' | bzip2 > testdata/fake_restic.bz2
func TestEnsureBinaryDownloadsVerifiesAndInstalls(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	raw, err := os.ReadFile("testdata/fake_restic.bz2")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(nil)
	t.Cleanup(srv.Close)
	srv.Config.Handler = fixtureHandler(raw)

	withTestChecksum(t, raw)

	p, err := EnsureBinary(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(p)
	if !bytes.Contains(content, []byte("fake-restic")) {
		t.Fatalf("installed binary content = %q", content)
	}
	info, _ := os.Stat(p)
	if info.Mode()&0o111 == 0 {
		t.Fatal("binary not executable")
	}
}

func TestBinPathHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := BinPath(); err == nil {
		t.Fatal("want error when HOME is unresolvable")
	}
}

func TestEnsureBinaryHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := EnsureBinary(context.Background(), "http://example.com"); err == nil {
		t.Fatal("want error when HOME is unresolvable")
	}
}

func TestEnsureBinaryUnsupportedArch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	prev, ok := checksums[runtime.GOARCH]
	delete(checksums, runtime.GOARCH)
	t.Cleanup(func() {
		if ok {
			checksums[runtime.GOARCH] = prev
		}
	})
	if _, err := EnsureBinary(context.Background(), "http://example.com"); err == nil {
		t.Fatal("want error for unsupported architecture")
	}
}

func TestEnsureBinaryRequestCreationError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// A control character in the URL makes http.NewRequestWithContext fail to parse it.
	if _, err := EnsureBinary(context.Background(), "http://example.com/\x7f"); err == nil {
		t.Fatal("want error when the request cannot be constructed")
	}
}

func TestEnsureBinaryNonOKStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	if _, err := EnsureBinary(context.Background(), srv.URL); err == nil {
		t.Fatal("want error on non-200 response")
	}
}

func TestEnsureBinaryBadBzip2Payload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	raw := []byte("not actually bzip2 data")
	srv := httptest.NewServer(fixtureHandler(raw))
	t.Cleanup(srv.Close)
	withTestChecksum(t, raw)

	if _, err := EnsureBinary(context.Background(), srv.URL); err == nil {
		t.Fatal("want decompression error for a bad bzip2 payload")
	}
}

func TestEnsureBinaryConnectionRefused(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := EnsureBinary(context.Background(), "http://127.0.0.1:1"); err == nil {
		t.Fatal("want error when the download server is unreachable")
	}
}

func TestEnsureBinaryMkdirAllFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Block .local/share/floe/bin with a regular file so MkdirAll fails.
	if err := os.MkdirAll(filepath.Join(home, ".local/share/floe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".local/share/floe/bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/fake_restic.bz2")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(fixtureHandler(raw))
	t.Cleanup(srv.Close)
	withTestChecksum(t, raw)

	if _, err := EnsureBinary(context.Background(), srv.URL); err == nil {
		t.Fatal("want error when the bin dir cannot be created")
	}
}

func TestEnsureBinaryWriteFileFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".local/share/floe/bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	raw, err := os.ReadFile("testdata/fake_restic.bz2")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(fixtureHandler(raw))
	t.Cleanup(srv.Close)
	withTestChecksum(t, raw)

	if _, err := EnsureBinary(context.Background(), srv.URL); err == nil {
		t.Fatal("want error when the binary file cannot be written")
	}
}

func TestEnsureBinaryRejectsBadChecksum(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	raw, _ := os.ReadFile("testdata/fake_restic.bz2")
	srv := httptest.NewServer(fixtureHandler(raw))
	t.Cleanup(srv.Close)
	withChecksum(t, "deadbeef")
	if _, err := EnsureBinary(context.Background(), srv.URL); err == nil {
		t.Fatal("want checksum error")
	}
}

func TestEnsureBinarySkipsWhenPresent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p, _ := BinPath()
	os.MkdirAll(filepathDir(p), 0o755)
	os.WriteFile(p, []byte("existing"), 0o755)
	got, err := EnsureBinary(context.Background(), "http://127.0.0.1:1") // unreachable: must not be hit
	if err != nil || got != p {
		t.Fatalf("got %q, %v", got, err)
	}
}

func fixtureHandler(raw []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Write(raw)
	}
}

func withTestChecksum(t *testing.T, raw []byte) {
	sum := sha256.Sum256(raw)
	withChecksum(t, hex.EncodeToString(sum[:]))
}

func withChecksum(t *testing.T, sum string) {
	prev, ok := checksums[runtime.GOARCH]
	checksums[runtime.GOARCH] = sum
	t.Cleanup(func() {
		if ok {
			checksums[runtime.GOARCH] = prev
		} else {
			delete(checksums, runtime.GOARCH)
		}
	})
}

func filepathDir(p string) string {
	return filepath.Dir(p)
}
