package config

import (
	"os"
	"path/filepath"
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
