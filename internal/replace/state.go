package replace

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// State is a Replace in progress. It pins the snapshot, so a resumed run restores the same one
// even after the backup got a newer snapshot, and restic skips what is already restored.
type State struct {
	Backup     string    `toml:"backup"` // name of the replaced Mac's backup
	Repo       string    `toml:"repo"`
	SnapshotID string    `toml:"snapshot_id"`
	OldHome    string    `toml:"old_home"`
	Started    time.Time `toml:"started"`
}

func statePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local/state/floe/replace.toml"), nil
}

// LoadState returns the Replace an earlier run left unfinished; false when there is none.
func LoadState() (State, bool, error) {
	path, err := statePath()
	if err != nil {
		return State{}, false, err
	}
	var s State
	if _, err := toml.DecodeFile(path, &s); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return State{}, false, nil
		}
		return State{}, false, fmt.Errorf("reading %s: %w", path, err)
	}
	return s, true, nil
}

// Save records s for a later floe init to resume.
func (s State) Save() error {
	path, err := statePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(s); err != nil {
		return err
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}

// ClearState forgets the Replace in progress, once it ended or the user declined to resume it.
func ClearState() error {
	path, err := statePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
