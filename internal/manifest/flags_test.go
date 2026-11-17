package manifest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/inventory"
	"github.com/Jszigeti/floe/internal/run"
)

const tapInfo = `[
  {"name": "homebrew/core", "official": true, "trusted": true},
  {"name": "sikarugir-app/sikarugir", "official": false, "trusted": true},
  {"name": "supabase/tap", "official": false, "trusted": false}
]`

const keychainDump = `keychain: "/Users/x/Library/Keychains/login.keychain-db"
class: "genp"
attributes:
    0x00000007 <blob>="Chrome Safe Storage"
    "svce"<blob>="Chrome Safe Storage"
class: "genp"
attributes:
    0x00000007 <blob>="Slack Safe Storage"
class: "genp"
attributes:
    0x00000007 <blob>="Chrome Safe Storage"
class: "inet"
attributes:
    0x00000007 <blob>="github.com"
    "acct"<blob>="Safe Storage"
`

// recipeTools answers every collector the way a healthy Mac does; fail
// names the commands (name + first arg) that must fail instead.
func recipeTools(home string, fail ...string) *run.Fake {
	return &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if slices.Contains(fail, c.Name+" "+c.Args[0]) {
			return "", errors.New("boom")
		}
		joined := c.Name + " " + strings.Join(c.Args, " ")
		switch {
		case c.Name == "plutil":
			data, err := os.ReadFile(c.Args[len(c.Args)-1])
			return string(data), err
		case joined == "mas list":
			return "1500855883  CapCut  (9.7.1)\n", nil
		case joined == "brew tap-info --json --installed":
			return tapInfo, nil
		case joined == "security dump-keychain "+filepath.Join(home, "Library/Keychains/login.keychain-db"):
			return keychainDump, nil
		case joined == "defaults read com.apple.finder FXICloudDriveDesktop":
			return "1\n", nil
		}
		return "", nil
	}}
}

func TestDumpWritesFlagsAndInventory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	marker := filepath.Join(home, "Pictures/Photos Library.photoslibrary/resources/cpl/cloudsync.noindex/cpl_enabled_marker")
	writeTestFile(t, marker, "2025-06-29")
	apps := t.TempDir()
	writeTestFile(t, filepath.Join(apps, "Voicebox.app/Contents/Info.plist"), `{"CFBundleIdentifier":"sh.voicebox.app"}`)
	dir := t.TempDir()
	f := recipeTools(home)

	got, _, err := Dump(context.Background(), f, dir, Options{AppRoots: []string{apps}, Casks: inventory.CaskIndex{"Google Chrome.app": "google-chrome"}, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0].Source != inventory.SourceBackup || got[0].BundleID != "sh.voicebox.app" {
		t.Fatalf("inventory = %+v, want Voicebox from backup", got)
	}
	saved, err := inventory.Read(filepath.Join(dir, "apps.json"))
	if err != nil || len(saved) != 1 || saved[0].Name != "Voicebox" {
		t.Fatalf("apps.json = %+v, %v", saved, err)
	}
	assertFile(t, filepath.Join(dir, "photos-icloud"), "true\n")
	assertFile(t, filepath.Join(dir, "desktop-documents-icloud"), "true\n")
	assertFile(t, filepath.Join(dir, "untrusted-taps.txt"), "supabase/tap\n")
	assertFile(t, filepath.Join(dir, "keychain-apps.txt"), "Chrome\nSlack\n")
	for _, c := range f.Calls {
		if c.Name == "security" && slices.Contains(c.Args, "-d") {
			t.Fatalf("the keychain is read without secrets, got %v", c.Args)
		}
	}
	failed, err := Failures(dir)
	if err != nil || len(failed) != 0 {
		t.Fatalf("failures = %v, %v, want none", failed, err)
	}
}

func TestDumpFlagsOffAndEmptyListsRemoved(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	for _, name := range []string{"untrusted-taps.txt", "keychain-apps.txt"} {
		writeTestFile(t, filepath.Join(dir, name), "stale\n")
	}
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		switch c.Name {
		case "defaults":
			return "", errors.New("defaults [read]: exit status 1: The domain/default pair of (com.apple.finder, FXICloudDriveDesktop) does not exist")
		case "brew":
			return "[]", nil
		}
		return "", nil
	}}

	if _, _, err := Dump(context.Background(), f, dir, opts(t.TempDir())); err != nil {
		t.Fatal(err)
	}

	assertFile(t, filepath.Join(dir, "photos-icloud"), "false\n")
	assertFile(t, filepath.Join(dir, "desktop-documents-icloud"), "false\n")
	for _, name := range []string{"untrusted-taps.txt", "keychain-apps.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("want stale %s removed when the list is empty, err = %v", name, err)
		}
	}
}

func TestDumpRecordsFirstFailureAndClearsOnSuccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	o := Options{Casks: inventory.CaskIndex{"x.app": "x"}}
	first := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

	for i, fail := range [][]string{{"brew bundle", "security dump-keychain"}, {"brew bundle"}} {
		o.Now = first.AddDate(0, 0, i)
		if _, _, err := Dump(context.Background(), recipeTools(home, fail...), dir, o); err != nil {
			t.Fatal(err)
		}
	}
	failed, err := Failures(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 || !failed["Brewfile"].Equal(first) {
		t.Fatalf("failures = %v, want only Brewfile since its first failure", failed)
	}
	if got := Incomplete(failed); got != "recipe incomplete since 2026-11-04: Brewfile failed" {
		t.Fatalf("Incomplete = %q", got)
	}

	o.Now = first.AddDate(0, 0, 2)
	if _, _, err := Dump(context.Background(), recipeTools(home), dir, o); err != nil {
		t.Fatal(err)
	}
	if failed, err := Failures(dir); err != nil || len(failed) != 0 {
		t.Fatalf("failures = %v, %v, want none after a full run", failed, err)
	}
}

func TestDumpWithoutCaskListIsIncomplete(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()

	if _, _, err := Dump(context.Background(), recipeTools(home), dir, opts(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	failed, err := Failures(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := failed["apps.json"]; !ok || len(failed) != 1 {
		t.Fatalf("failures = %v, want apps.json only", failed)
	}
}

func TestDumpMissingToolIsNotAFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	notFound := fmt.Errorf("mas [list]: %w", &exec.Error{Name: "mas", Err: exec.ErrNotFound})
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if c.Name == "mas" {
			return "", notFound
		}
		return recipeTools(home).Handler(c)
	}}

	_, warnings, err := Dump(context.Background(), f, dir, Options{Casks: inventory.CaskIndex{"x.app": "x"}, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if failed, _ := Failures(dir); len(failed) != 0 {
		t.Fatalf("failures = %v, a tool that is not installed has nothing to record", failed)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "mas.txt") {
		t.Fatalf("warnings = %v, want the missing tool logged", warnings)
	}
}

func TestDumpUnreadableFlagIsAFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "desktop-documents-icloud"), "true\n")
	pictures := filepath.Join(home, "Pictures")
	writeTestFile(t, filepath.Join(pictures, "x"), "")
	if err := os.Chmod(pictures, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(pictures, 0o755) })
	if _, _, err := Dump(context.Background(), recipeTools(home, "defaults read"), dir, Options{Casks: inventory.CaskIndex{"x.app": "x"}, Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	failed, _ := Failures(dir)
	if _, ok := failed["photos-icloud"]; !ok {
		t.Fatalf("failures = %v, want photos-icloud", failed)
	}
	if _, ok := failed["desktop-documents-icloud"]; !ok {
		t.Fatalf("failures = %v, want desktop-documents-icloud", failed)
	}
	if _, err := os.Stat(filepath.Join(dir, "desktop-documents-icloud")); !os.IsNotExist(err) {
		t.Fatalf("want the stale flag removed, err = %v", err)
	}
}

func TestDumpBadTapInfoIsAFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		if c.Name == "brew" && c.Args[0] == "tap-info" {
			return "Error: not json", nil
		}
		return recipeTools(home).Handler(c)
	}}

	if _, _, err := Dump(context.Background(), f, dir, Options{Casks: inventory.CaskIndex{"x.app": "x"}, Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if failed, _ := Failures(dir); len(failed) != 1 {
		t.Fatalf("failures = %v, want untrusted-taps.txt", failed)
	}
}

func TestFailuresReadsCorruptStatus(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "status.json"), "{")
	if _, err := Failures(dir); err == nil {
		t.Fatal("want an error for a corrupt status.json")
	}
	writeTestFile(t, filepath.Join(dir, "status.json"), "{}")
	if failed, err := Failures(dir); err != nil || failed == nil {
		t.Fatalf("failures = %v, %v, want an empty map", failed, err)
	}
}

func TestIncompleteNamesEveryCollectorSinceTheOldest(t *testing.T) {
	if got := Incomplete(nil); got != "" {
		t.Fatalf("Incomplete(nil) = %q", got)
	}
	got := Incomplete(map[string]time.Time{
		"mas.txt":  time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC),
		"Brewfile": time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC),
	})
	if got != "recipe incomplete since 2026-11-04: Brewfile, mas.txt failed" {
		t.Fatalf("Incomplete = %q", got)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A Replace reads the flags and lists back as Dump wrote them; a backup made before one existed has none.
func TestReadFlagAndListReadWhatDumpWrote(t *testing.T) {
	dir := t.TempDir()
	if err := writeFlag(filepath.Join(dir, PhotosFlag), true, nil); err != nil {
		t.Fatal(err)
	}
	if err := writeFlag(filepath.Join(dir, DesktopDocumentsFlag), false, nil); err != nil {
		t.Fatal(err)
	}
	if err := writeLines(filepath.Join(dir, KeychainAppsFile), []string{"Chrome", "Slack"}); err != nil {
		t.Fatal(err)
	}
	if !ReadFlag(dir, PhotosFlag) || ReadFlag(dir, DesktopDocumentsFlag) || ReadFlag(dir, "missing") {
		t.Errorf("flags = %v %v %v, want true false false", ReadFlag(dir, PhotosFlag), ReadFlag(dir, DesktopDocumentsFlag), ReadFlag(dir, "missing"))
	}
	if got, want := ReadList(dir, KeychainAppsFile), []string{"Chrome", "Slack"}; !slices.Equal(got, want) {
		t.Errorf("ReadList = %q, want %q", got, want)
	}
	if got := ReadList(dir, UntrustedTapsFile); got != nil {
		t.Errorf("missing list = %q, want none", got)
	}
}
