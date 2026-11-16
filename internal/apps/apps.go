// Package apps reinstalls a Mac's apps from the recipe a backup carries (see internal/manifest)
// and the app inventory (see internal/inventory).
package apps

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Jszigeti/floe/internal/run"
)

// HomebrewInstall is Homebrew's official installer.
const HomebrewInstall = `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`

// BrewDirs are where Homebrew puts brew and mas; a shell opened before installing it lacks them on its PATH.
var BrewDirs = []string{"/opt/homebrew/bin", "/usr/local/bin"}

// Deps is what Install touches, so tests point it at temporary folders.
type Deps struct {
	Runner   run.Runner
	BrewDirs []string
	AppsRoot string
	Admin    *Admin // nil when the user gave no administrator password
}

// Progress is how far Install got, sent on every line brew or mas prints.
type Progress struct {
	Done, Total int
	Current     string
}

// Report is what Install leaves to the user.
type Report struct {
	BrewFailed  []string // Brewfile entries and casks Homebrew could not install
	StoreFailed []string // App Store ids mas could not install
	Missing     []string // Expected entries still absent from AppsRoot
	Commands    []string // commands to run by hand later
	Notes       []string // why a step left a command, when no failed entry says it
}

// entries counts what a Brewfile installs: every line but taps, blanks and comments.
func entries(file string) int {
	if file == "" {
		return 0
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if k := kind(line); k != "" && k != "tap" {
			n++
		}
	}
	return n
}

// findTool looks on PATH first, then where Homebrew installs.
func findTool(name string, dirs []string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, dir := range dirs {
		if p := filepath.Join(dir, name); fileExists(p) {
			return p
		}
	}
	return ""
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// masIDs reads the app ids of `mas list` output, "497799835  Xcode  (15.0)".
func masIDs(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var ids []string
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			if _, err := strconv.Atoi(f[0]); err == nil {
				ids = append(ids, f[0])
			}
		}
	}
	return ids
}

// missingApps lists the names absent from appsRoot.
func missingApps(names []string, appsRoot string) []string {
	var missing []string
	for _, name := range names {
		if !fileExists(filepath.Join(appsRoot, name)) {
			missing = append(missing, name)
		}
	}
	return missing
}
