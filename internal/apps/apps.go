// Package apps reinstalls a Mac's apps from the recipe a backup carries (see internal/manifest)
// and the app inventory (see internal/inventory).
package apps

import (
	"cmp"
	"os"
	"os/exec"
	"path/filepath"
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
	// At numbers the entry at work, 1..Total; 0 between entries and while Homebrew installs.
	At      int
	Current string
	Tool    string // the tool at work, as the screen names it
}

// Report is what Install leaves to the user.
type Report struct {
	BrewFailed  []string   // Brewfile entries and casks Homebrew could not install
	StoreFailed []string   // App Store ids mas could not install
	Missing     []string   // Expected entries still absent from AppsRoot
	Leftovers   []Leftover // steps left, offered as end-of-replace actions and saved in the report
	Notes       []string   // why a step left a command, when no failed entry says it
	Homebrew    string     // why Homebrew is missing, "" when it is there
	// VS Code extensions Homebrew could not install, apart since no app comes back with them.
	ExtensionsFailed []string
	// Apps installed or already there, per source: casks and App Store ids, not formulae or extensions.
	FromHomebrew, FromAppStore int
}

// LeftoverKind is which step of Install is left.
type LeftoverKind int

const (
	LeftHomebrew   LeftoverKind = iota + 1 // Homebrew itself
	LeftBrewfile                           // the Brewfile's apps and tools
	LeftExtensions                         // the VS Code extensions
	LeftCasks                              // inventory casks, Items holding their tokens
	LeftAppStore                           // App Store apps, Items holding their ids
)

// Leftover is a step Install left, with the shell command that does it.
type Leftover struct {
	Kind    LeftoverKind
	Items   []string // the casks or App Store ids it installs, nil for the other kinds
	Command string
}

// StoreLeftover installs the App Store apps ids, and mas first unless masThere.
func StoreLeftover(ids []string, masThere bool) Leftover {
	command := "mas install " + strings.Join(ids, " ")
	if !masThere {
		command = "brew install mas && " + command
	}
	return Leftover{Kind: LeftAppStore, Items: ids, Command: command}
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

// BrewDownloads is the folder `brew --cache` downloads into, under ~/Library/Caches/Homebrew
// unless HOMEBREW_CACHE moves it. A brew.env file that moves it is not read.
func BrewDownloads(home string) string {
	return filepath.Join(cmp.Or(os.Getenv("HOMEBREW_CACHE"), filepath.Join(home, "Library/Caches/Homebrew")), "downloads")
}

// DownloadedBytes is what the files of dir hold. brew's curl writes a download there as it
// comes, even when it prints nothing.
func DownloadedBytes(dir string) int64 {
	files, _ := os.ReadDir(dir) // no folder yet: nothing downloaded
	var n int64
	for _, e := range files {
		if info, err := e.Info(); err == nil {
			n += info.Size()
		}
	}
	return n
}

// FindTool looks on PATH first, then where Homebrew installs.
func FindTool(name string, dirs []string) string {
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
