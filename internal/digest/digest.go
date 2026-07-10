package digest

import (
	"os"
	"path/filepath"
	"time"
)

const interval = 7 * 24 * time.Hour

func stampPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local/state/floe/last_digest"), nil
}

func Due(now time.Time) bool {
	p, err := stampPath()
	if err != nil {
		return false
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return true
	}
	last, err := time.Parse(time.RFC3339, string(raw))
	if err != nil {
		return true
	}
	return now.Sub(last) >= interval
}

func MarkSent(now time.Time) error {
	p, err := stampPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(now.Format(time.RFC3339)), 0o644)
}
