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
