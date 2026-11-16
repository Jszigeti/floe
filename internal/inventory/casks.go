package inventory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// CaskURL is Homebrew's public cask list.
const CaskURL = "https://formulae.brew.sh/api/cask.json"

// CaskIndex maps an app file name ("Google Chrome.app") to its cask token.
type CaskIndex map[string]string

// LoadCasks returns the cask index from cachePath, refreshed from url when
// the cache is older than maxAge. A failed refresh keeps the cache; with
// neither, the index is empty and the error nil.
func LoadCasks(ctx context.Context, cachePath, url string, maxAge time.Duration, now time.Time) (CaskIndex, error) {
	if fi, err := os.Stat(cachePath); err == nil && now.Sub(fi.ModTime()) < maxAge {
		if index, err := readCasks(cachePath); err == nil {
			return index, nil
		}
	}
	data, index, err := download(ctx, url)
	if err != nil {
		cached, _ := readCasks(cachePath) // no cache: an empty index, which backs up nothing
		return cached, nil
	}
	if err := writeCache(cachePath, data); err != nil {
		return index, fmt.Errorf("saving the cask list: %w", err)
	}
	return index, nil
}

func download(ctx context.Context, url string) ([]byte, CaskIndex, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	index, err := parseCasks(data)
	return data, index, err
}

func readCasks(path string) (CaskIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CaskIndex{}, err
	}
	return parseCasks(data)
}

type cask struct {
	Token     string                       `json:"token"`
	Artifacts []map[string]json.RawMessage `json:"artifacts"`
}

// parseCasks keeps the installed name of every `app` artifact: the source
// file name, or the {"target": …} that follows it. The first cask wins, so
// "google-chrome" beats "google-chrome@dev".
func parseCasks(data []byte) (CaskIndex, error) {
	var casks []cask
	if err := json.Unmarshal(data, &casks); err != nil {
		return CaskIndex{}, err
	}
	index := CaskIndex{}
	for _, c := range casks {
		for _, a := range c.Artifacts {
			for _, name := range appNames(a["app"]) {
				if _, taken := index[name]; !taken {
					index[name] = c.Token
				}
			}
		}
	}
	return index, nil
}

func appNames(raw json.RawMessage) []string {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	var names []string
	for _, item := range items {
		var source string
		var target struct {
			Target string `json:"target"`
		}
		switch {
		case json.Unmarshal(item, &source) == nil:
			names = append(names, filepath.Base(source))
		case json.Unmarshal(item, &target) == nil && target.Target != "" && len(names) > 0:
			names[len(names)-1] = filepath.Base(target.Target)
		}
	}
	return names
}

// writeCache replaces the cache through a temporary file, so a crash never leaves half a list.
func writeCache(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
