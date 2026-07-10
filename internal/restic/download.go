package restic

import (
	"bytes"
	"compress/bzip2"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

const Version = "0.19.1" // pinned; update via Step 1 of the download task

var checksums = map[string]string{
	"arm64": "7be0a144ccc377880f294204aa271d76e4b79554b42a751151d425ce6ebac143",
	"amd64": "c38d579622cf602f665234c5a8c315030b6cf70656028fe6dc29a786b60e5f35",
}

const defaultBaseURL = "https://github.com/restic/restic/releases/download/v" + Version

func BinPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local/share/floe/bin/restic"), nil
}

func EnsureBinary(ctx context.Context, baseURL string) (string, error) {
	dst, err := BinPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(dst); err == nil {
		return dst, nil
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	want, ok := checksums[runtime.GOARCH]
	if !ok {
		return "", fmt.Errorf("unsupported architecture %s", runtime.GOARCH)
	}
	url := fmt.Sprintf("%s/restic_%s_darwin_%s.bz2", baseURL, Version, runtime.GOARCH)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading restic: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading restic: HTTP %d from %s", resp.StatusCode, url)
	}
	compressed, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(compressed)
	if hex.EncodeToString(sum[:]) != want {
		return "", fmt.Errorf("restic checksum mismatch for %s", url)
	}
	binary, err := io.ReadAll(bzip2.NewReader(bytes.NewReader(compressed)))
	if err != nil {
		return "", fmt.Errorf("decompressing restic: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, binary, 0o755); err != nil {
		return "", err
	}
	return dst, nil
}
