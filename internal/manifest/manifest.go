// Package manifest dumps a tiny reinstall recipe of the system (apps, brew,
// mas, crontab) into text files inside the user's home directory, so the
// daily snapshot picks them up alongside everything else.
package manifest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jszigeti/floe/internal/run"
)

// Dir returns $HOME/.local/state/floe/manifest.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local/state/floe/manifest"), nil
}

// Dump writes the reinstall recipe into dir. Best-effort by design: a
// missing tool must never fail the nightly backup — each collector that
// fails is skipped and reported as a warning. err is non-nil only if dir
// cannot be created. appsRoot is the applications folder to list
// (production callers pass "/Applications").
func Dump(ctx context.Context, r run.Runner, dir, appsRoot string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var warnings []string
	brewfile := filepath.Join(dir, "Brewfile")
	if _, err := r.Run(ctx, run.Cmd{Name: "brew", Args: []string{"bundle", "dump", "--force", "--file", brewfile}}); err != nil {
		os.Remove(brewfile)
		warnings = append(warnings, fmt.Sprintf("brew bundle dump failed: %v", err))
	}
	if w := dumpCommand(ctx, r, "mas", []string{"list"}, filepath.Join(dir, "mas.txt")); w != "" {
		warnings = append(warnings, w)
	}
	if w := dumpCommand(ctx, r, "crontab", []string{"-l"}, filepath.Join(dir, "crontab.txt")); w != "" {
		warnings = append(warnings, w)
	}
	dumpApplications(appsRoot, filepath.Join(dir, "applications.txt"))
	return warnings, nil
}

// dumpCommand writes the tool's output to path, or removes a stale path if
// the tool fails or produces no output, so yesterday's recipe never survives
// into a new snapshot. It returns a one-line warning when the tool failed.
func dumpCommand(ctx context.Context, r run.Runner, name string, args []string, path string) string {
	out, err := r.Run(ctx, run.Cmd{Name: name, Args: args})
	if err != nil || out == "" {
		os.Remove(path) // ignore not-exist
		if err != nil {
			return fmt.Sprintf("%s %s failed: %v", name, strings.Join(args, " "), err)
		}
		return ""
	}
	os.WriteFile(path, []byte(out), 0o644)
	return ""
}

func dumpApplications(appsRoot, path string) {
	entries, err := os.ReadDir(appsRoot)
	if err != nil {
		return
	}
	var out string
	for _, e := range entries {
		out += e.Name() + "\n"
	}
	os.WriteFile(path, []byte(out), 0o644)
}
