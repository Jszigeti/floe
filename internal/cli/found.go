package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Jszigeti/floe/internal/config"
)

// ownRepo is the repository in dest of a config this Mac wrote, or of one from before
// v0.2.0 that records no Mac; "" otherwise.
func ownRepo(dest, mac string) string {
	prev, err := config.Load()
	if err != nil || (prev.Machine != "" && prev.Machine != mac) {
		return ""
	}
	repo, err := prev.RepoPath()
	if err != nil || filepath.Dir(repo) != filepath.Join(dest, "Floe.backup") {
		return ""
	}
	return repo
}

// freeRepoPath names a new repository after this Mac, adding -2, -3… instead of reusing a
// folder that exists: another Mac's backup or a half-synced one.
func freeRepoPath(dest string) (string, error) {
	name, err := config.MachineName()
	if err != nil {
		return "", err
	}
	base := filepath.Join(dest, "Floe.backup", name)
	path := base
	for i := 2; ; i++ {
		_, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			return path, nil
		}
		if err != nil {
			return "", err
		}
		path = fmt.Sprintf("%s-%d", base, i)
	}
}
