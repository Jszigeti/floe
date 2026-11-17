//go:build integration

package integration

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/inventory"
	"github.com/Jszigeti/floe/internal/replace"
	"github.com/Jszigeti/floe/internal/restic"
	"github.com/Jszigeti/floe/internal/run"
)

// The same verdicts as the unit test's matcher, so restic and the matcher agree on them.
var (
	ruleKept = []string{
		"notes.txt",
		"Desktop/todo.txt",
		"Library/Application Support/Steam/userdata/save",
		"Library/Application Support/Steam/com.apple.inside",
		"Library/Application Support/Foo [beta]/x",
		"Library/Application Support/$HOME/x",
		"Library/Application Support/trail /x",
		"Library/Containers/com.docker.docker/Data/x",
		"Library/Containers/com.docker.helper/x",
		"Library/Group Containers/UBF8T346G9.Office/x",
		"Library/Group Containers/group.com.docker.docker/x",
		"Library/Preferences/com.google.Chrome.plist",
		"Library/LaunchAgents/com.google.keystone.agent.plist",
		"Library/Fonts/Inter.ttf",
		"Library/Keyboard Layouts/x.keylayout",
	}
	ruleDropped = []string{
		".Trash/old.txt",
		".local/state/floe/manifest/apps.json",
		"Pictures/Photos Library.photoslibrary/database/Photos.sqlite",
		"Library/.localized",
		"Library/Application Support/com.apple.TCC/TCC.db",
		"Library/Application Support/networkserviceproxy/x",
		"Library/Application Support/CloudKit.store-wal",
		"Library/Application Support/Already There/x",
		"Library/Application Support/Foo b/x",
		"Library/Application Support/trail/x",
		"Library/Application Support/1A2B3C4D-0000-1111-2222-333344445555/x",
		"Library/Containers/com.raycast.macos.RaycastAppIntents/x",
		"Library/Containers/com.apple.Notes/x",
		"Library/Group Containers/group.com.apple.notes/x",
		"Library/Group Containers/ZZZZZZZZZZ.leftover/x",
		"Library/Preferences/ByHost/.GlobalPreferences.1A2B3C4D-0000-1111-2222-333344445555.plist",
		"Library/Preferences/com.apple.dock.plist",
		"Library/LaunchAgents/com.floe.backup.plist",
		"Library/Keychains/login.keychain-db",
		"Library/Mail/V10/x",
		"Library/Caches/x",
		"Library/Logs/floe.log",
	}
)

func TestExcludePatternsMatchRestic(t *testing.T) {
	ctx, home, client := newTestRepo(t)
	old := filepath.Join(home, "old")
	for _, p := range append(slices.Clone(ruleKept), ruleDropped...) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(old, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(old, p), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.Backup(ctx, []string{old}, restic.BackupOptions{}); err != nil {
		t.Fatal(err)
	}
	snaps, err := client.Snapshots(ctx)
	if err != nil || len(snaps) != 1 {
		t.Fatalf("snapshots = %+v (%v), want 1", snaps, err)
	}
	id := snaps[0].ShortID

	var listing []string
	if err := client.List(ctx, id, filepath.Join(old, "Library"), func(e restic.Entry) { listing = append(listing, e.Path) }); err != nil {
		t.Fatal(err)
	}
	patterns := replace.ExcludePatterns(replace.RuleInput{
		OldHome: old, Library: listing,
		Present: map[string]bool{"Already There": true, "Foo b": true, "trail": true},
		Apps: []inventory.App{
			{BundleID: "com.docker.docker", Nested: []string{"com.docker.helper"}},
			{BundleID: "com.google.Chrome"},
			{BundleID: "com.microsoft.VSCode", Team: "UBF8T346G9"},
		},
		PhotosICloud: true,
	})
	excludeFile := filepath.Join(home, "exclude.txt")
	if err := os.WriteFile(excludeFile, []byte(strings.Join(patterns, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(home, "new")
	if _, err := (run.ExecRunner{}).Run(ctx, run.Cmd{
		Name: client.Bin,
		Args: []string{"restore", id + ":" + old, "--target", target, "--exclude-file", excludeFile},
		Env:  []string{"RESTIC_REPOSITORY=" + client.Repo, "RESTIC_PASSWORD=" + client.Pass},
	}); err != nil {
		t.Fatal(err)
	}

	var got []string
	if err := filepath.WalkDir(target, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(target, p)
			got = append(got, rel)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	want := slices.Sorted(slices.Values(ruleKept))
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("restored files:\n%s\nwant:\n%s\npatterns:\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"), strings.Join(patterns, "\n"))
	}
}
