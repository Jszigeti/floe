// Package manifest dumps a tiny reinstall recipe of the system (apps, brew,
// mas, crontab, a few iCloud flags) into files inside the user's home
// directory, so the daily snapshot picks them up alongside everything else.
package manifest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/inventory"
	"github.com/Jszigeti/floe/internal/run"
)

// Dir returns $HOME/.local/state/floe/manifest.
func Dir() (string, error) {
	return config.StatePath("manifest")
}

// The recipe's files, as Dump names them in the manifest folder.
const (
	BrewFile             = "Brewfile"
	MasFile              = "mas.txt"
	ApplicationsFile     = "applications.txt"
	AppsFile             = "apps.json"
	UntrustedTapsFile    = "untrusted-taps.txt"
	KeychainAppsFile     = "keychain-apps.txt"
	PhotosFlag           = "photos-icloud"
	DesktopDocumentsFlag = "desktop-documents-icloud"
)

// RecipeFiles are the recipe files a Replace copies out of the old Mac's backup.
var RecipeFiles = []string{BrewFile, MasFile, ApplicationsFile, AppsFile, PhotosFlag, DesktopDocumentsFlag, KeychainAppsFile, UntrustedTapsFile}

type Options struct {
	// AppRoots are the folders whose app bundles are inventoried; applications.txt lists the first one, as older floe reads it.
	AppRoots []string
	Casks    inventory.CaskIndex
	Now      time.Time
}

// Dump writes the reinstall recipe into dir and returns the app inventory.
// Best-effort by design: a collector that fails never fails the nightly
// backup, it becomes a warning and an entry in status.json. err is non-nil
// only if dir cannot be created.
func Dump(ctx context.Context, r run.Runner, dir string, o Options) ([]inventory.App, []string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, err
	}
	c := collection{failed: map[string]bool{}}
	c.note(BrewFile, dumpBrewfile(ctx, r, filepath.Join(dir, BrewFile)))
	mas, err := dumpCommand(ctx, r, "mas", []string{"list"}, filepath.Join(dir, MasFile))
	c.note(MasFile, err)
	// crontab -l exits 1 when the user simply has no crontab: an empty recipe, not a failure.
	if _, err := dumpCommand(ctx, r, "crontab", []string{"-l"}, filepath.Join(dir, "crontab.txt")); err == nil || !strings.Contains(err.Error(), "no crontab for") {
		c.note("crontab.txt", err)
	}
	if len(o.AppRoots) > 0 {
		c.note(ApplicationsFile, dumpApplications(o.AppRoots[0], filepath.Join(dir, ApplicationsFile)))
	}
	apps, err := dumpInventory(ctx, r, filepath.Join(dir, AppsFile), o, mas)
	c.note(AppsFile, err)
	c.note(UntrustedTapsFile, dumpUntrustedTaps(ctx, r, filepath.Join(dir, UntrustedTapsFile)))
	c.note(KeychainAppsFile, dumpKeychainApps(ctx, r, home, filepath.Join(dir, KeychainAppsFile)))
	photos, err := photosICloud(home)
	c.note(PhotosFlag, writeFlag(filepath.Join(dir, PhotosFlag), photos, err))
	desktop, err := DesktopDocumentsICloud(ctx, r)
	c.note(DesktopDocumentsFlag, writeFlag(filepath.Join(dir, DesktopDocumentsFlag), desktop, err))
	if err := recordFailures(dir, c.failed, o.Now); err != nil {
		c.warnings = append(c.warnings, fmt.Sprintf("status.json: %v", err))
	}
	return apps, c.warnings, nil
}

type collection struct {
	warnings []string
	failed   map[string]bool
}

// A tool that is not installed leaves nothing to record (no Homebrew, no
// mas): it is logged but never reported as an incomplete recipe.
func (c *collection) note(name string, err error) {
	if err == nil {
		return
	}
	c.warnings = append(c.warnings, fmt.Sprintf("%s: %v", name, err))
	if !errors.Is(err, exec.ErrNotFound) {
		c.failed[name] = true
	}
}

func dumpBrewfile(ctx context.Context, r run.Runner, path string) error {
	if _, err := r.Run(ctx, run.Cmd{Name: "brew", Args: []string{"bundle", "dump", "--force", "--file", path}}); err != nil {
		_ = os.Remove(path) // ignore not-exist
		return fmt.Errorf("brew bundle dump failed: %w", err)
	}
	return nil
}

// dumpCommand writes the tool's output to path, or removes a stale path if
// the tool fails or produces no output, so yesterday's recipe never survives
// into a new snapshot.
func dumpCommand(ctx context.Context, r run.Runner, name string, args []string, path string) (string, error) {
	out, err := r.Run(ctx, run.Cmd{Name: name, Args: args})
	if err != nil {
		_ = os.Remove(path) // ignore not-exist
		return "", fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return out, writeOrRemove(path, out)
}

func dumpApplications(appsRoot, path string) error {
	entries, err := os.ReadDir(appsRoot)
	if err != nil {
		return nil
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	out := strings.Join(names, "\n")
	if out != "" {
		out += "\n"
	}
	return os.WriteFile(path, []byte(out), 0o644)
}

// dumpInventory writes apps.json. Without a cask list every app is
// "unknown" and none is saved in the apps snapshot, which is a failure.
func dumpInventory(ctx context.Context, r run.Runner, path string, o Options, masList string) ([]inventory.App, error) {
	apps, err := inventory.Scan(ctx, r, o.AppRoots, o.Casks, masList)
	if err != nil {
		_ = os.Remove(path) // ignore not-exist
		return nil, err
	}
	if err := inventory.Write(path, apps); err != nil {
		return apps, err
	}
	if len(o.Casks) == 0 {
		return apps, errors.New("no Homebrew cask list, apps are not saved")
	}
	return apps, nil
}

// writeOrRemove writes content to path, or removes path when content is empty.
func writeOrRemove(path, content string) error {
	if content == "" {
		_ = os.Remove(path) // ignore not-exist
		return nil
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
