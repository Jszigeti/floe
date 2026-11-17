package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	home, _ := os.UserHomeDir()
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if c.Destination != ICloudDrivePath(home) {
		t.Fatalf("dest = %q", c.Destination)
	}
	if c.Hour != 13 || c.Retention.Daily != 7 || c.Retention.Weekly != 4 || c.Retention.Monthly != 6 {
		t.Fatalf("defaults = %+v", c)
	}
	if !slices.Contains(c.Scope.Paths, home) {
		t.Fatalf("paths = %v", c.Scope.Paths)
	}
	if !slices.Contains(c.Scope.Exclude, "node_modules") {
		t.Fatal("want node_modules excluded by default")
	}
	if !slices.Contains(c.Scope.Exclude, filepath.Join(home, "Library/Mobile Documents")) {
		t.Fatal("want iCloud Drive excluded from sources")
	}
	if !slices.Contains(c.Scope.Exclude, "floe-restore-*") {
		t.Fatal("want floe-restore dirs excluded by default")
	}
	if !slices.Contains(c.Scope.Exclude, ".*.floe-restore") {
		t.Fatal("want the copies an interrupted replace left beside an app excluded by default")
	}
	if !slices.Contains(c.Scope.Exclude, filepath.Join(home, "Library/Biome")) {
		t.Fatal("want Library/Biome excluded by default")
	}
	if !slices.Contains(c.Scope.Exclude, filepath.Join(home, ".vscode/extensions")) {
		t.Fatal("want .vscode/extensions excluded by default")
	}
	if !slices.Contains(c.Scope.Exclude, ".pytest_cache") {
		t.Fatal("want .pytest_cache excluded by default")
	}
	if !slices.Contains(c.Scope.Exclude, filepath.Join(home, "Library/pnpm")) {
		t.Fatal("want Library/pnpm excluded by default")
	}
	if !slices.Contains(c.Scope.Exclude, "state.vscdb.backup") {
		t.Fatal("want state.vscdb.backup excluded by default")
	}
	if !slices.Contains(c.Scope.Exclude, "GPUCache") {
		t.Fatal("want chromium GPUCache excluded by default")
	}
	if !slices.Contains(c.Scope.Exclude, filepath.Join(home, ".yarn/berry/cache")) {
		t.Fatal("want yarn berry cache excluded by default")
	}
	if !slices.Contains(c.Scope.Exclude, filepath.Join(home, "Library/Application Support/FileProvider/*/wharf")) {
		t.Fatal("want FileProvider wharf staging area excluded by default")
	}
	if !slices.Contains(c.Scope.Exclude, filepath.Join(home, "Library/Group Containers/group.com.apple.CoreSpeech/Caches")) {
		t.Fatal("want CoreSpeech caches excluded by default")
	}
	if !slices.Contains(c.Scope.Exclude, filepath.Join(home, "Library/Group Containers/group.com.apple.secure-control-center-preferences/Library/Preferences")) {
		t.Fatal("want secure-control-center-preferences unreadable preferences excluded by default")
	}
}

func TestDefaultHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := Default(); err == nil {
		t.Fatal("want error when HOME is unresolvable")
	}
}

func TestPathHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := Path(); err == nil {
		t.Fatal("want error when HOME is unresolvable")
	}
}

func TestStatePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got, err := StatePath("cask.json"); err != nil || got != filepath.Join(home, ".local/state/floe/cask.json") {
		t.Errorf("StatePath = %q, %v; want %q", got, err, filepath.Join(home, ".local/state/floe/cask.json"))
	}
	t.Setenv("HOME", "")
	if _, err := StatePath("cask.json"); err == nil {
		t.Error("want error when HOME is unresolvable")
	}
}

func TestLoadHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := Load(); err == nil {
		t.Fatal("want error when HOME is unresolvable")
	}
}

func TestLoadMalformedTOML(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("not = [valid toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Load()
	if err == nil || !strings.Contains(err.Error(), "reading") {
		t.Fatalf("err = %v, want a wrapped reading error", err)
	}
}

func TestNormalizeDestinationHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := NormalizeDestination("~/dest"); err == nil {
		t.Fatal("want error when HOME is unresolvable")
	}
}

func TestNormalizeDestinationExpandsHomeVar(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := NormalizeDestination("$HOME/dest")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "dest"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got, err = NormalizeDestination("$HOME")
	if err != nil {
		t.Fatal(err)
	}
	if got != home {
		t.Fatalf("got %q, want %q", got, home)
	}
}

func TestSavePathHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	if err := (Config{Destination: "/tmp/x"}).Save(); err == nil {
		t.Fatal("want error when HOME is unresolvable")
	}
}

func TestSaveMkdirAllFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Block the .config path with a regular file so MkdirAll(.config/floe) fails.
	if err := os.WriteFile(filepath.Join(home, ".config"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (Config{Destination: "/tmp/x"}).Save(); err == nil {
		t.Fatal("want error when the config dir cannot be created")
	}
}

func TestSaveCreateFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config/floe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if err := (Config{Destination: "/tmp/x"}).Save(); err == nil {
		t.Fatal("want error when the config file cannot be created")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c, _ := Default()
	c.Hour = 4
	c.Hooks.Pre = []string{"echo hi"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Hour != 4 || len(got.Hooks.Pre) != 1 || got.Hooks.Pre[0] != "echo hi" {
		t.Fatalf("loaded = %+v", got)
	}
}

func TestLoadNormalizesTildeDestination(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	home, _ := os.UserHomeDir()
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`destination = "~/dest"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "dest")
	if c.Destination != want {
		t.Fatalf("destination = %q, want %q", c.Destination, want)
	}
}

func TestLoadRejectsRelativeDestination(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`destination = "relative/dest"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("want error for relative destination")
	}
}

func TestLoadMissingFileIsActionable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "floe init") {
		t.Fatalf("err = %v, want hint to run floe init", err)
	}
}

func TestRepoPathSanitizesHostname(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c, _ := Default()
	c.Destination = "/tmp/dest"
	p, err := c.RepoPath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p, "/tmp/dest/Floe.backup/") {
		t.Fatalf("repo = %q", p)
	}
	base := filepath.Base(p)
	if strings.Contains(base, " ") || strings.HasSuffix(base, ".local") {
		t.Fatalf("hostname not sanitized: %q", base)
	}
}

func TestLoadRejectsEmptyDestination(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`destination = ""`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Load()
	if err == nil {
		t.Fatal("want error for empty destination")
	}
	if !strings.Contains(err.Error(), "floe init") {
		t.Fatalf("err = %v, want mention of floe init", err)
	}
}

func TestRepoPathPrefersTheRecordedRepo(t *testing.T) {
	c := Config{Destination: "/dest", Repo: "/dest/Floe.backup/Old-Name"}
	got, err := c.RepoPath()
	if err != nil || got != "/dest/Floe.backup/Old-Name" {
		t.Fatalf("RepoPath = %q, %v", got, err)
	}
}

func TestSaveLoadKeepsIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	c.Repo, c.Machine = "/dest/Floe.backup/Mac", "UUID-1"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Repo != c.Repo || got.Machine != c.Machine {
		t.Fatalf("loaded repo = %q, machine = %q", got.Repo, got.Machine)
	}
}

func TestParseReadsTOML(t *testing.T) {
	c, err := Parse("hour = 9\n[scope]\npaths = [\"/Users/alice\"]\n")
	if err != nil || c.Hour != 9 || !slices.Equal(c.Scope.Paths, []string{"/Users/alice"}) {
		t.Fatalf("Parse = %+v, %v", c, err)
	}
	if _, err := Parse("hour = "); err == nil {
		t.Fatal("want an error for broken TOML")
	}
}

func TestRehomeMovesEveryPathUnderTheOldHome(t *testing.T) {
	c := Config{
		Destination: "/Users/alice/Library/Mobile Documents/com~apple~CloudDocs",
		Hour:        9,
		Scope: Scope{
			Paths:   []string{"/Users/alice"},
			Exclude: []string{"/Users/alice/Library/Caches", "node_modules", "/Users/alice2/x", "/Users/alice-work/y", "/Users/alice.old"},
		},
		Hooks: Hooks{Pre: []string{"sh /Users/alice/bin/export.sh /Users/alice/out"}},
	}
	want := Config{
		Destination: "/Users/bob/Library/Mobile Documents/com~apple~CloudDocs",
		Hour:        9,
		Scope: Scope{
			Paths:   []string{"/Users/bob"},
			Exclude: []string{"/Users/bob/Library/Caches", "node_modules", "/Users/alice2/x", "/Users/alice-work/y", "/Users/alice.old"},
		},
		Hooks: Hooks{Pre: []string{"sh /Users/bob/bin/export.sh /Users/bob/out"}},
	}
	if got := c.Rehome("/Users/alice", "/Users/bob"); !reflect.DeepEqual(got, want) {
		t.Fatalf("Rehome = %+v\nwant     %+v", got, want)
	}
	if c.Scope.Paths[0] != "/Users/alice" {
		t.Fatal("Rehome must leave the receiver's slices untouched")
	}
}
