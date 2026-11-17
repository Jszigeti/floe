package digest

import (
	"os"
	"path/filepath"
	"time"
)

const interval = 7 * 24 * time.Hour

// StatePath is where floe keeps the state file named name, notice stamps included.
func StatePath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local/state/floe", name), nil
}

// NoticeDue reports whether the notice named name has not fired within the
// last 7 days.
func NoticeDue(name string, now time.Time) bool {
	p, err := StatePath(name)
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

// MarkNotice records that the notice named name fired at now.
func MarkNotice(name string, now time.Time) error {
	p, err := StatePath(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(now.Format(time.RFC3339)), 0o644)
}

func Due(now time.Time) bool {
	return NoticeDue("last_digest", now)
}

func MarkSent(now time.Time) error {
	return MarkNotice("last_digest", now)
}
