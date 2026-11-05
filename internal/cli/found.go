package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

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

// recentlySaved is how fresh a backup is when its Mac may still be writing to it.
const recentlySaved = 48 * time.Hour

type foundBackup struct {
	Name      string
	Path      string
	LastSaved time.Time
}

// findBackups lists dest's repositories that hold a snapshot, newest first. It reads
// modification times only, which iCloud placeholders keep without being downloaded.
func findBackups(dest string) ([]foundBackup, error) {
	root := filepath.Join(dest, "Floe.backup")
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var found []foundBackup
	for _, e := range entries {
		path := filepath.Join(root, e.Name())
		if !e.IsDir() {
			continue
		}
		if last := lastSnapshot(path); !last.IsZero() {
			found = append(found, foundBackup{Name: e.Name(), Path: path, LastSaved: last})
		}
	}
	slices.SortFunc(found, func(a, b foundBackup) int { return b.LastSaved.Compare(a.LastSaved) })
	return found, nil
}

// lastSnapshot is the newest modification time in repo/snapshots, zero when it holds none.
// Hidden files, a Finder .DS_Store for one, are not snapshots.
func lastSnapshot(repo string) time.Time {
	entries, err := os.ReadDir(filepath.Join(repo, "snapshots"))
	if err != nil {
		return time.Time{}
	}
	var last time.Time
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().After(last) {
			last = info.ModTime()
		}
	}
	return last
}

// chooseBackup asks whether this Mac, named mac, replaces one of found, newest first; false
// adds it alongside.
func chooseBackup(p prompter, found []foundBackup, mac string, now time.Time) (foundBackup, bool, error) {
	alongside := len(found) + 1
	if len(found) == 1 {
		b := found[0]
		p.cmd.Printf("Found a backup: %s, last saved %s.\n", b.Name, b.LastSaved.Format(time.DateOnly))
		p.cmd.Printf("  1. Replace %s: restore its files here and continue its backup\n", b.Name)
		p.cmd.Printf("  2. Add this Mac alongside: new backup, %s keeps its own\n", b.Name)
	} else {
		p.cmd.Printf("Found %d backups.\n", len(found))
		for i, b := range found {
			p.cmd.Printf("  %d. Replace %s, last saved %s: restore its files here and continue its backup\n", i+1, b.Name, b.LastSaved.Format(time.DateOnly))
		}
		p.cmd.Printf("  %d. Add this Mac alongside: new backup, the others keep their own\n", alongside)
	}
	def := alongside
	// A folder named after this Mac is most likely its own backup, after its config was deleted:
	// Enter must never restore an old snapshot over newer files.
	if now.Sub(found[0].LastSaved) > recentlySaved && found[0].Name != mac {
		def = 1
	}
	n, err := strconv.Atoi(p.ask("Choice", strconv.Itoa(def)))
	if err != nil || n < 1 || n > alongside {
		return foundBackup{}, false, fmt.Errorf("choice must be a number from 1 to %d", alongside)
	}
	if n == alongside {
		return foundBackup{}, false, nil
	}
	b := found[n-1]
	if now.Sub(b.LastSaved) <= recentlySaved {
		p.cmd.Printf("%s was saved less than 2 days ago. On that Mac, run `floe backup`, then `floe uninstall`, so it stops writing into this backup.\n", b.Name)
		if p.ask("Done on the other Mac? (y/n)", "n") != "y" {
			return foundBackup{}, false, errors.New("stopped: run floe backup, then floe uninstall on the other Mac, then floe init here again")
		}
	}
	return b, true, nil
}
