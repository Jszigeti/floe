// Package manifest dumps a tiny reinstall recipe of the system (apps, brew,
// mas, vscode extensions, crontab) into text files inside the user's home
// directory, so the daily snapshot picks them up alongside everything else.
package manifest

import (
	"context"
	"os"
	"path/filepath"

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
// fails or whose tool is absent is skipped. Returns an error only if dir
// cannot be created. appsRoot is the applications folder to list
// (production callers pass "/Applications").
func Dump(ctx context.Context, r run.Runner, dir, appsRoot string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	r.Run(ctx, run.Cmd{Name: "brew", Args: []string{"bundle", "dump", "--force", "--file", filepath.Join(dir, "Brewfile")}})
	dumpCommand(ctx, r, "mas", []string{"list"}, filepath.Join(dir, "mas.txt"))
	dumpCommand(ctx, r, "crontab", []string{"-l"}, filepath.Join(dir, "crontab.txt"))
	dumpApplications(appsRoot, filepath.Join(dir, "applications.txt"))
	return nil
}

func dumpCommand(ctx context.Context, r run.Runner, name string, args []string, path string) {
	out, err := r.Run(ctx, run.Cmd{Name: name, Args: args})
	if err != nil || out == "" {
		return
	}
	os.WriteFile(path, []byte(out), 0o644)
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
