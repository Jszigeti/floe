package apps

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/run"
)

// testDeps hides the machine's own brew and mas behind an empty PATH and empty Homebrew folders.
func testDeps(t *testing.T, f *run.Fake) Deps {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	return Deps{Runner: f, Out: io.Discard, BrewDirs: []string{t.TempDir()}, AppsRoot: t.TempDir()}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// installTool puts an executable named name in dir, as Homebrew would.
func installTool(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	writeFile(t, p, "#!/bin/sh\n", 0o755)
	return p
}

func TestReinstallInstallsHomebrewThenTheBrewfile(t *testing.T) {
	f := &run.Fake{}
	d := testDeps(t, f)
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "/bin/bash" {
			installTool(t, d.BrewDirs[0], "brew")
		}
		return "", nil
	}
	recipe := t.TempDir()
	brewfile := filepath.Join(recipe, "Brewfile")
	writeFile(t, brewfile, "brew \"jq\"\n", 0o644)

	rep := Reinstall(context.Background(), d, recipe)

	if len(f.Calls) != 2 || f.Calls[0].Name != "/bin/bash" || f.Calls[0].Args[1] != HomebrewInstall || !f.Calls[0].Terminal {
		t.Fatalf("first call must be the Homebrew installer on the terminal: %+v", f.Calls)
	}
	if f.Calls[1].Name != filepath.Join(d.BrewDirs[0], "brew") || !slices.Equal(f.Calls[1].Args, []string{"bundle", "install", "--file", brewfile}) {
		t.Fatalf("second call = %+v", f.Calls[1])
	}
	if len(rep.Commands) != 0 || len(rep.BrewFailed) != 0 {
		t.Fatalf("report = %+v", rep)
	}
}

func TestReinstallGoesOnWhenHomebrewFailsToInstall(t *testing.T) {
	f := &run.Fake{Default: run.Response{Err: errors.New("curl: could not resolve host")}}
	d := testDeps(t, f)
	recipe := t.TempDir()
	brewfile := filepath.Join(recipe, "Brewfile")
	writeFile(t, brewfile, "brew \"jq\"\n", 0o644)
	writeFile(t, filepath.Join(recipe, "applications.txt"), "Zed.app\n", 0o644)

	rep := Reinstall(context.Background(), d, recipe)

	if len(f.Calls) != 1 {
		t.Fatalf("only the installer can run without brew: %+v", f.Calls)
	}
	if !slices.Equal(rep.Commands, []string{HomebrewInstall, "brew bundle install --file " + brewfile}) || !slices.Equal(rep.Missing, []string{"Zed.app"}) {
		t.Fatalf("report = %+v", rep)
	}
}

func TestReinstallRunsBrewThenMasAndKeepsGoing(t *testing.T) {
	f := &run.Fake{}
	d := testDeps(t, f)
	brew := installTool(t, d.BrewDirs[0], "brew")
	mas := installTool(t, d.BrewDirs[0], "mas")
	f.Handler = func(c run.Cmd) (string, error) {
		switch {
		case c.Name == brew:
			return "", errors.New("brew [bundle install]: exit status 1: Installing jq has failed!\n`brew bundle` failed! 1 Brewfile dependency failed to install")
		case c.Name == mas && c.Args[1] == "222":
			return "", errors.New("mas [install 222]: exit status 1: download failed")
		}
		return "", nil
	}
	recipe := t.TempDir()
	brewfile := filepath.Join(recipe, "Brewfile")
	writeFile(t, brewfile, "brew \"jq\"\nbrew \"git\"\n", 0o644)
	writeFile(t, filepath.Join(recipe, "mas.txt"), "111  Keynote  (15.2)\n 222  Numbers  (15.2)\n333  Pages  (15.2)\n", 0o644)

	rep := Reinstall(context.Background(), d, recipe)

	order := make([]string, len(f.Calls))
	for i, c := range f.Calls {
		order[i] = filepath.Base(c.Name) + " " + strings.Join(c.Args, " ")
	}
	want := []string{"brew bundle install --file " + brewfile, "mas install 111", "mas install 222", "mas install 333"}
	if !slices.Equal(order, want) {
		t.Fatalf("calls = %q, want %q", order, want)
	}
	if !slices.Equal(rep.BrewFailed, []string{"jq"}) || !slices.Equal(rep.Commands, []string{"brew bundle install --file " + brewfile, "mas install 222"}) {
		t.Fatalf("report = %+v", rep)
	}
}

func TestReinstallWithoutMasPrintsItsCommand(t *testing.T) {
	f := &run.Fake{}
	d := testDeps(t, f)
	installTool(t, d.BrewDirs[0], "brew")
	recipe := t.TempDir()
	writeFile(t, filepath.Join(recipe, "mas.txt"), "111  Keynote  (15.2)\n", 0o644)

	rep := Reinstall(context.Background(), d, recipe)

	if len(f.Calls) != 0 || !slices.Equal(rep.Commands, []string{"brew install mas && mas install 111"}) {
		t.Fatalf("calls = %+v, report = %+v", f.Calls, rep)
	}
}

func TestFindToolPrefersPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	want := installTool(t, dir, "brew")
	if got := findTool("brew", []string{t.TempDir()}); got != want {
		t.Fatalf("findTool = %q, want %q", got, want)
	}
}

func TestMasIDsKeepsNumericIDsOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mas.txt")
	writeFile(t, path, "111  Keynote  (15.2)\n\nNo installed apps found\n 222  Numbers  (15.2)\n", 0o644)
	if got := masIDs(path); !slices.Equal(got, []string{"111", "222"}) {
		t.Fatalf("masIDs = %q", got)
	}
}

func TestMissingAppsSkipsHiddenEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Safari.app"), 0o755); err != nil {
		t.Fatal(err)
	}
	list := filepath.Join(t.TempDir(), "applications.txt")
	writeFile(t, list, ".DS_Store\n.localized\nSafari.app\nZed.app\n", 0o644)
	if got := missingApps(list, root); !slices.Equal(got, []string{"Zed.app"}) {
		t.Fatalf("missingApps = %q", got)
	}
	if got := missingApps(filepath.Join(root, "none.txt"), root); got != nil {
		t.Fatalf("no list must mean nothing missing, got %q", got)
	}
}
