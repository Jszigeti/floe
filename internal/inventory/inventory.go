// Package inventory records every app bundle of the Mac and where it comes
// from, so a new Mac can reinstall it or get its bundle back from a backup.
package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Jszigeti/floe/internal/run"
)

type Source string

const (
	SourceAppStore Source = "appstore"
	SourceCask     Source = "cask"
	SourceBackup   Source = "backup"
	SourceSystem   Source = "system"
	// SourceUnknown: neither reinstalled nor backed up, when no cask list was available or the app sits in an installer subfolder.
	SourceUnknown Source = "unknown"
)

type App struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	BundleID   string   `json:"bundle_id,omitempty"`
	Executable string   `json:"executable,omitempty"`
	Version    string   `json:"version,omitempty"`
	Team       string   `json:"team,omitempty"`
	Nested     []string `json:"nested,omitempty"` // bundle ids inside the bundle
	Size       int64    `json:"size"`
	Source     Source   `json:"source"`
	Cask       string   `json:"cask,omitempty"`     // token when Source == "cask"
	StoreID    string   `json:"store_id,omitempty"` // App Store id when Source == "appstore"
}

// Scan inventories the .app bundles in each root and its first-level
// subfolders. masList is the output of `mas list`. A missing root is skipped.
func Scan(ctx context.Context, r run.Runner, roots []string, casks CaskIndex, masList string) ([]App, error) {
	var apps []App
	for _, root := range roots {
		paths, err := bundles(root)
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			apps = append(apps, scanApp(ctx, r, p, casks, masList, filepath.Dir(p) == root))
		}
	}
	return apps, nil
}

// bundles lists root/*.app and root/*/*.app, skipping files and symlinks.
func bundles(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		p := filepath.Join(root, e.Name())
		switch {
		case !e.IsDir():
		case strings.HasSuffix(e.Name(), ".app"):
			paths = append(paths, p)
		default:
			sub, _ := os.ReadDir(p) // an unreadable subfolder holds no app we can record
			for _, s := range sub {
				if s.IsDir() && strings.HasSuffix(s.Name(), ".app") {
					paths = append(paths, filepath.Join(p, s.Name()))
				}
			}
		}
	}
	return paths, nil
}

type infoPlist struct {
	BundleID    string `json:"CFBundleIdentifier"`
	Executable  string `json:"CFBundleExecutable"`
	Version     string `json:"CFBundleShortVersionString"`
	Name        string `json:"CFBundleName"`
	DisplayName string `json:"CFBundleDisplayName"`
}

// Apps in a subfolder were put there by an installer (Python, Logi Options+): they are recorded,
// never saved on their own, since restoring them without their installer would not run.
func scanApp(ctx context.Context, r run.Runner, path string, casks CaskIndex, masList string, topLevel bool) App {
	file := filepath.Base(path)
	info := readInfo(ctx, r, filepath.Join(path, "Contents", "Info.plist"))
	team, apple := signature(ctx, r, path)
	app := App{
		Name:       strings.TrimSuffix(file, ".app"),
		Path:       path,
		BundleID:   info.BundleID,
		Executable: info.Executable,
		Version:    info.Version,
		Team:       team,
	}
	app.Nested, app.Size = walkBundle(ctx, r, path)
	switch {
	case exists(filepath.Join(path, "Contents", "_MASReceipt")):
		app.Source = SourceAppStore
		app.StoreID = storeID(masList, app.Name, info.Name, info.DisplayName)
	case apple:
		app.Source = SourceSystem
	case casks[file] != "":
		app.Source = SourceCask
		app.Cask = casks[file]
	case len(casks) > 0 && topLevel:
		app.Source = SourceBackup
	default:
		app.Source = SourceUnknown
	}
	return app
}

// readInfo reads an Info.plist, XML or binary, as JSON; an unreadable one gives empty fields.
func readInfo(ctx context.Context, r run.Runner, path string) infoPlist {
	var info infoPlist
	out, err := r.Run(ctx, run.Cmd{Name: "plutil", Args: []string{"-convert", "json", "-o", "-", path}})
	if err == nil {
		_ = json.Unmarshal([]byte(out), &info) // a plist that is not a dictionary gives empty fields
	}
	return info
}

// signature returns the code-signing team id and whether Apple signed the
// bundle itself. codesign prints its details on stderr, hence the shell.
func signature(ctx context.Context, r run.Runner, path string) (team string, apple bool) {
	out, _ := r.Run(ctx, run.Cmd{Name: "sh", Args: []string{"-c", `codesign -dvv "$1" 2>&1`, "sh", path}}) // unsigned: no team, not Apple
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(line, "TeamIdentifier="); ok && v != "not set" {
			team = v
		}
		if strings.HasPrefix(line, "Authority=") && strings.HasSuffix(line, "Software Signing") {
			apple = true
		}
	}
	return team, apple
}

// walkBundle returns the sorted bundle ids of every Info.plist nested in
// the bundle, and the bundle's size in bytes.
func walkBundle(ctx context.Context, r run.Runner, path string) ([]string, int64) {
	top := filepath.Join(path, "Contents", "Info.plist")
	var nested []string
	var size int64
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil // an unreadable entry only lowers the size
		}
		if fi, err := d.Info(); err == nil {
			size += fi.Size()
		}
		if d.Name() == "Info.plist" && p != top {
			if id := readInfo(ctx, r, p).BundleID; id != "" && !slices.Contains(nested, id) {
				nested = append(nested, id)
			}
		}
		return nil
	})
	slices.Sort(nested)
	return nested, size
}

// storeID finds the App Store id of an app in `mas list` output,
// "361285480  Keynote  (15.2.1)", matching any of the app's names.
func storeID(masList string, names ...string) string {
	for _, line := range strings.Split(masList, "\n") {
		id, rest, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		name := strings.TrimSpace(rest)
		if i := strings.LastIndex(name, "("); i > 0 {
			name = strings.TrimSpace(name[:i])
		}
		if slices.Contains(names, name) {
			return id
		}
	}
	return ""
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Write saves the inventory as indented JSON.
func Write(path string, apps []App) error {
	data, err := json.MarshalIndent(apps, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Read loads an inventory saved by Write.
func Read(path string) ([]App, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var apps []App
	return apps, json.Unmarshal(data, &apps)
}
