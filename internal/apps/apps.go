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
	Commands    []string // commands left, offered as end-of-replace actions and saved in the report
	Notes       []string // why a step left a command, when no failed entry says it
	Homebrew    string   // why Homebrew is missing, "" when it is there
	// Apps installed or already there, per source: casks and App Store ids, not formulae or extensions.
	FromHomebrew, FromAppStore int
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

type masEntry struct{ id, name string }

// masEntries reads `mas list` output, "497799835  Xcode  (15.0)".
func masEntries(path string) []masEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var list []masEntry
	for _, line := range strings.Split(string(data), "\n") {
		id, rest, _ := strings.Cut(strings.TrimSpace(line), " ")
		if _, err := strconv.Atoi(id); err != nil {
			continue
		}
		if i := strings.LastIndex(rest, "("); i > 0 {
			rest = rest[:i]
		}
		list = append(list, masEntry{id, strings.TrimSpace(rest)})
	}
	return list
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
