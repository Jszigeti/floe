package replace

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Jszigeti/floe/internal/inventory"
)

const ruleHome = "/Users/old"

// ruleApps are the inventoried apps of the fake old Mac: Raycast is not one of them.
var ruleApps = []inventory.App{
	{Name: "Docker", BundleID: "com.docker.docker", Nested: []string{"com.docker.helper"}, Team: "9BNSXJN65R"},
	{Name: "Google Chrome", BundleID: "com.google.Chrome", Team: "EQHXZ8M8AV"},
	{Name: "Visual Studio Code", BundleID: "com.microsoft.VSCode", Team: "UBF8T346G9"},
	{Name: "Keynote", BundleID: "com.apple.iWork.Keynote", Source: inventory.SourceAppStore},
}

// ruleKept and ruleDropped are paths of the fake old home, relative to it.
var ruleKept = []string{
	"notes.txt",
	"Code/app/main.go",
	".local/bin/other",
	"Pictures/Photos Library.photoslibrary/database/Photos.sqlite",
	"Desktop/todo.txt",
	"Documents/letter.pages",
	"Library/Application Support/Steam/userdata/save",
	"Library/Application Support/Colossal Order/x",
	"Library/Application Support/zoxide/db.zo",
	"Library/Application Support/Arc/x",
	"Library/Application Support/Foo [beta]/x",
	"Library/Application Support/$HOME/x",
	"Library/Application Support/trail /x",
	"Library/Containers/com.docker.docker/Data/x",
	"Library/Containers/com.docker.helper/x",
	"Library/Group Containers/UBF8T346G9.Office/x",
	"Library/Group Containers/group.com.docker.docker/x",
	"Library/Preferences/com.google.Chrome.plist",
	"Library/Preferences/com.docker.helper.plist",
	"Library/LaunchAgents/com.google.keystone.agent.plist",
	"Library/Fonts/Inter.ttf",
	"Library/Keyboard Layouts/x.keylayout",
	"Library/Services/x.workflow/Contents/document.wflow",
}

var ruleDropped = []string{
	".Trash/old.txt",
	".local/bin/floe",
	".local/share/floe/bin/restic",
	".local/state/floe/manifest/apps.json",
	".config/floe/config.toml",
	"Library/.localized",
	"Library/Application Support/com.apple.TCC/TCC.db",
	"Library/Application Support/networkserviceproxy/x",
	"Library/Application Support/CloudKit.store-wal",
	"Library/Application Support/AddressBook/x",
	"Library/Application Support/AskPermission/x",
	"Library/Application Support/familycircled/x",
	"Library/Application Support/Already There/x",
	"Library/Application Support/steam-like/x",
	"Library/Application Support/1A2B3C4D-0000-1111-2222-333344445555/x",
	"Library/Application Support/Foo b/x",
	"Library/Application Support/trail/x",
	"Library/Containers/com.raycast.macos.RaycastAppIntents/x",
	"Library/Containers/com.apple.Notes/x",
	"Library/Containers/com.apple.iWork.Keynote/x",
	"Library/Group Containers/group.com.apple.notes/x",
	"Library/Group Containers/group.com.leftover.app/x",
	"Library/Group Containers/ZZZZZZZZZZ.leftover/x",
	"Library/Preferences/ByHost/.GlobalPreferences.1A2B3C4D-0000-1111-2222-333344445555.plist",
	"Library/Preferences/com.apple.dock.plist",
	"Library/Preferences/com.raycast.macos.plist",
	"Library/Preferences/com.google.Chrome.plist.lockfile",
	"Library/LaunchAgents/com.apple.something.plist",
	"Library/LaunchAgents/com.floe.backup.plist",
	"Library/LaunchAgents/readme.txt",
	"Library/Keychains/login.keychain-db",
	"Library/Accounts/Accounts4.sqlite",
	"Library/Mail/V10/x",
	"Library/Messages/chat.db",
	"Library/Caches/x",
	"Library/Logs/floe.log",
	"Library/Metadata/x",
	"Library/Mobile Documents/com~apple~CloudDocs/x",
	"Library/CloudStorage/x",
	"Library/Saved Application State/x",
}

// rulePresent also holds names an unescaped pattern of a kept name would match.
var rulePresent = map[string]bool{"Already There": true, "Steam-Like": true, "Foo b": true, "trail": true}

// ruleListing is what restic lists under <old home>/Library for the fake home,
// absolute, the requested folder first.
func ruleListing() []string {
	seen := map[string]bool{}
	listing := []string{ruleHome + "/Library"}
	for _, p := range append(append([]string{}, ruleKept...), ruleDropped...) {
		if !strings.HasPrefix(p, "Library/") {
			continue
		}
		for d := p; d != "Library"; d = filepath.Dir(d) {
			if !seen[d] {
				seen[d] = true
				listing = append(listing, ruleHome+"/"+d)
			}
		}
	}
	return listing
}

func TestExcludePatterns(t *testing.T) {
	photos := "Pictures/Photos Library.photoslibrary/database/Photos.sqlite"
	cases := []struct {
		name             string
		photos, deskDocs bool
		extraDrop        []string
	}{
		{name: "nothing in iCloud"},
		{name: "iCloud Photos", photos: true, extraDrop: []string{photos}},
		{name: "Desktop and Documents in iCloud", deskDocs: true, extraDrop: []string{"Desktop/todo.txt", "Documents/letter.pages"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			patterns := ExcludePatterns(RuleInput{
				OldHome: ruleHome, Library: ruleListing(), Present: rulePresent, Apps: ruleApps,
				PhotosICloud: c.photos, DesktopDocumentsICloud: c.deskDocs,
			})
			for _, p := range ruleKept {
				want := !slices.Contains(c.extraDrop, p)
				if got := restored(t, patterns, p); got != want {
					t.Errorf("%s restored = %v, want %v", p, got, want)
				}
			}
			for _, p := range ruleDropped {
				if restored(t, patterns, p) {
					t.Errorf("%s restored, want dropped", p)
				}
			}
			if t.Failed() {
				t.Logf("patterns:\n%s", strings.Join(patterns, "\n"))
			}
		})
	}
}

func TestExcludePatternsIgnoresDeeperEntriesAndRelativePaths(t *testing.T) {
	in := RuleInput{OldHome: ruleHome, Apps: ruleApps, Library: []string{
		ruleHome + "/Library",
		"Library/Fonts",
		ruleHome + "/Library/Fonts/Inter.ttf",
		ruleHome + "/Library/Application Support",
		"Library/Application Support/Steam",
		"Library/Application Support/Steam/com.apple.x",
	}}
	patterns := ExcludePatterns(in)
	for _, p := range []string{"Library/Fonts/Inter.ttf", "Library/Application Support/Steam/com.apple.x"} {
		if !restored(t, patterns, p) {
			t.Errorf("%s dropped, want restored:\n%s", p, strings.Join(patterns, "\n"))
		}
	}
}

func TestExcludePatternsNeverSplitsALine(t *testing.T) {
	patterns := ExcludePatterns(RuleInput{OldHome: ruleHome, Library: []string{
		"Library/Application Support", "Library/Application Support/evil\nnotes.txt",
	}})
	if restored(t, patterns, "Library/Application Support/evil\nnotes.txt/x") {
		t.Error("a name holding a newline came back")
	}
	if !restored(t, patterns, "notes.txt") {
		t.Errorf("a name holding a newline broke the file into another pattern:\n%s", strings.Join(patterns, "\n"))
	}
}

// restored applies restic's restore rules to path, relative to the old home:
// each line read as restic reads an exclude file (trimmed, environment
// expanded), the last matching pattern wins, `!` re-includes, and a pattern
// matches a path when it matches its first components. An excluded folder
// is never entered, so nothing inside it can be re-included.
func restored(t *testing.T, patterns []string, path string) bool {
	t.Helper()
	var lines []string
	for _, p := range patterns {
		lines = append(lines, strings.Split(p, "\n")...)
	}
	parts := strings.Split(path, "/")
	for depth := 1; depth <= len(parts); depth++ {
		if excludedAt(t, lines, parts[:depth]) {
			return false
		}
	}
	return true
}

func excludedAt(t *testing.T, lines, parts []string) bool {
	t.Helper()
	excluded := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		line = os.ExpandEnv(line)
		negated := strings.HasPrefix(line, "!")
		line = strings.TrimPrefix(line, "!")
		if !strings.HasPrefix(line, "/") {
			// Unanchored patterns match at any depth in restic; the rule never writes one.
			t.Fatalf("pattern %q is not anchored at the old home", line)
		}
		pattern := strings.Split(line[1:], "/")
		if len(pattern) > len(parts) || !componentsMatch(t, pattern, parts) {
			continue
		}
		excluded = !negated
	}
	return excluded
}

func componentsMatch(t *testing.T, pattern, parts []string) bool {
	t.Helper()
	for i, p := range pattern {
		ok, err := filepath.Match(p, parts[i])
		if err != nil {
			t.Fatalf("pattern component %q: %v", p, err)
		}
		if !ok {
			return false
		}
	}
	return true
}
