package manifest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/run"
)

func TestDumpWritesRecipeFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	appsRoot := t.TempDir()
	for _, name := range []string{"Safari.app", "Notes.app"} {
		if err := os.Mkdir(filepath.Join(appsRoot, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f := &run.Fake{Responses: map[string]run.Response{
		"mas list":   {Out: "App 1\n"},
		"crontab -l": {Out: "@daily true\n"},
	}}

	if _, err := Dump(context.Background(), f, dir, appsRoot); err != nil {
		t.Fatal(err)
	}

	if !f.CalledWith("brew", "bundle", "dump") {
		t.Fatalf("want brew bundle dump call, got %+v", f.Calls)
	}
	found := false
	for _, c := range f.Calls {
		if c.Name != "brew" {
			continue
		}
		for _, a := range c.Args {
			if filepath.Base(a) == "Brewfile" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("want brew call with a path ending Brewfile, got %+v", f.Calls)
	}

	assertFile(t, filepath.Join(dir, "mas.txt"), "App 1\n")
	assertFile(t, filepath.Join(dir, "crontab.txt"), "@daily true\n")

	raw, err := os.ReadFile(filepath.Join(dir, "applications.txt"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	if !strings.Contains(content, "Safari.app") || !strings.Contains(content, "Notes.app") {
		t.Fatalf("applications.txt = %q, want both apps listed", content)
	}
}

func TestDumpToleratesMissingTools(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	appsRoot := t.TempDir()
	f := &run.Fake{Default: run.Response{Err: errors.New("no such tool")}}

	warnings, err := Dump(context.Background(), f, dir, appsRoot)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"mas.txt", "crontab.txt", "Brewfile"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should not exist when its tool fails, err = %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "applications.txt")); err != nil {
		t.Fatalf("applications.txt should exist even when empty: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("want warnings when every collector's tool fails")
	}
}

func TestDumpRemovesStaleFileWhenToolFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	appsRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "crontab.txt"), []byte("@daily true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if c.Name == "crontab" {
			return "", errors.New("crontab: no crontab for user")
		}
		return "", nil
	}}

	warnings, err := Dump(context.Background(), f, dir, appsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "crontab.txt")); !os.IsNotExist(err) {
		t.Fatalf("want stale crontab.txt removed after tool failure, err = %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("want a warning for the failed crontab collector")
	}
}

func TestDirJoinsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(home, ".local/state/floe/manifest") {
		t.Fatalf("dir = %q", dir)
	}
}

func TestDirHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := Dir(); err == nil {
		t.Fatal("want error when HOME is unresolvable")
	}
}

func TestDumpMkdirAllFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	parent := t.TempDir()
	blocker := filepath.Join(parent, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(blocker, "manifest") // blocker is a file, not a dir
	f := &run.Fake{}

	if _, err := Dump(context.Background(), f, dir, t.TempDir()); err == nil {
		t.Fatal("want error when the manifest dir cannot be created")
	}
}

func TestDumpApplicationsSkipsMissingRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	f := &run.Fake{}

	if _, err := Dump(context.Background(), f, dir, filepath.Join(dir, "no-such-apps-root")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "applications.txt")); !os.IsNotExist(err) {
		t.Fatalf("want no applications.txt when appsRoot cannot be read, err = %v", err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if string(raw) != want {
		t.Fatalf("%s = %q, want %q", path, raw, want)
	}
}
