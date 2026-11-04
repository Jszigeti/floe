// Package apps reinstalls a Mac's apps from the recipe a backup carries (see internal/manifest).
package apps

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Jszigeti/floe/internal/run"
)

// HomebrewInstall is Homebrew's official installer.
const HomebrewInstall = `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`

// BrewDirs are where Homebrew puts brew and mas; a shell opened before installing it lacks them on its PATH.
var BrewDirs = []string{"/opt/homebrew/bin", "/usr/local/bin"}

// brewFailedRe matches brew bundle's per-entry failure, "Installing <name> has failed!".
var brewFailedRe = regexp.MustCompile(`\w+ (\S+) has failed!`)

// Deps is what Reinstall touches, so tests point it at temporary folders.
type Deps struct {
	Runner   run.Runner
	Out      io.Writer
	BrewDirs []string
	AppsRoot string
}

// Report is what Reinstall leaves to the user.
type Report struct {
	BrewFailed []string // Brewfile entries brew bundle could not install
	Missing    []string // applications.txt entries still absent from AppsRoot
	Commands   []string // commands to run by hand later
}

// Reinstall replays the recipe in dir: Homebrew, the Brewfile, the App Store apps. A failing
// step never stops the next one; the Report says what is left.
func Reinstall(ctx context.Context, d Deps, dir string) Report {
	var rep Report
	brew := findTool("brew", d.BrewDirs)
	if brew == "" {
		_, _ = fmt.Fprintln(d.Out, "→ Installing Homebrew, it asks for your password…")
		if _, err := d.Runner.Run(ctx, run.Cmd{Name: "/bin/bash", Args: []string{"-c", HomebrewInstall}, Terminal: true}); err != nil {
			rep.Commands = append(rep.Commands, HomebrewInstall)
		}
		brew = findTool("brew", d.BrewDirs)
	}
	brewfile := filepath.Join(dir, "Brewfile")
	if _, err := os.Stat(brewfile); err == nil {
		if failed, ok := installBrewfile(ctx, d, brew, brewfile); !ok {
			rep.BrewFailed = failed
			rep.Commands = append(rep.Commands, "brew bundle install --file "+brewfile)
		}
	}
	if ids := masIDs(filepath.Join(dir, "mas.txt")); len(ids) > 0 {
		rep.Commands = append(rep.Commands, installMas(ctx, d, ids)...)
	}
	rep.Missing = missingApps(filepath.Join(dir, "applications.txt"), d.AppsRoot)
	return rep
}

// installBrewfile runs brew bundle; false, with the entries that failed, when it did not complete.
func installBrewfile(ctx context.Context, d Deps, brew, brewfile string) ([]string, bool) {
	if brew == "" {
		return nil, false
	}
	_, _ = fmt.Fprintln(d.Out, "→ Installing the Homebrew apps…")
	_, err := d.Runner.Run(ctx, run.Cmd{Name: brew, Args: []string{"bundle", "install", "--file", brewfile}, Terminal: true})
	if err == nil {
		return nil, true
	}
	var failed []string
	for _, m := range brewFailedRe.FindAllStringSubmatch(err.Error(), -1) {
		failed = append(failed, m[1])
	}
	return failed, false
}

// installMas installs the App Store apps one by one and returns the command for those that
// failed. mas 7 gives no signed-out signal of its own, so the message covers that case.
func installMas(ctx context.Context, d Deps, ids []string) []string {
	mas := findTool("mas", d.BrewDirs)
	if mas == "" {
		return []string{"brew install mas && mas install " + strings.Join(ids, " ")}
	}
	_, _ = fmt.Fprintln(d.Out, "→ Installing the App Store apps, sign in to the App Store if it asks…")
	var failed []string
	for _, id := range ids {
		if _, err := d.Runner.Run(ctx, run.Cmd{Name: mas, Args: []string{"install", id}, Terminal: true}); err != nil {
			failed = append(failed, id)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	_, _ = fmt.Fprintln(d.Out, "! Some App Store apps did not install. If the App Store is signed out, sign in to it, then run the command below.")
	return []string{"mas install " + strings.Join(failed, " ")}
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

// missingApps lists the applications.txt entries absent from appsRoot, hidden files aside.
func missingApps(path, appsRoot string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var missing []string
	for _, name := range strings.Split(string(data), "\n") {
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		if !fileExists(filepath.Join(appsRoot, name)) {
			missing = append(missing, name)
		}
	}
	return missing
}
