package replace

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Jszigeti/floe/internal/inventory"
)

// RuleInput is what decides which parts of the old home a Replace restores.
type RuleInput struct {
	OldHome string
	// Library lists the old home's Library: its entries and those of Application Support,
	// Containers, Group Containers, Preferences and LaunchAgents count, deeper ones are ignored.
	// Paths are absolute under OldHome or relative to it, as restic ls prints them or trimmed.
	Library []string
	// Present holds the names in the new home's Library/Application Support (macOS created them).
	Present                              map[string]bool
	Apps                                 []inventory.App
	PhotosICloud, DesktopDocumentsICloud bool
}

// sortedLibrary are the ~/Library folders that come back entry by entry, wholeLibrary those that come back whole.
var (
	sortedLibrary = []string{"Application Support", "Containers", "Group Containers", "Preferences", "LaunchAgents"}
	wholeLibrary  = []string{
		"Fonts", "Keyboard Layouts", "Input Methods", "Services", "Scripts", "Workflows",
		"Sounds", "Spelling", "ColorPickers", "Colors",
	}
)

var floePaths = []string{
	"/.local/bin/floe", "/.local/share/floe", "/.local/state/floe", "/.config/floe",
	"/Library/LaunchAgents/com.floe.backup.plist", "/Library/Logs/floe.log",
}

var uuidRe = regexp.MustCompile(`[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}`)

// ExcludePatterns returns the lines of a restic --exclude-file for `restore <id>:<old home>`.
// restic applies the last matching line, `!` re-includes, and an excluded folder is never
// entered, so the whole Library is excluded, then each kept folder re-included, then
// emptied and refilled with the entries that come back.
func ExcludePatterns(in RuleInput) []string {
	patterns := []string{"/.Trash"}
	if in.PhotosICloud {
		patterns = append(patterns, "/Pictures/Photos Library.photoslibrary")
	}
	if in.DesktopDocumentsICloud {
		patterns = append(patterns, "/Desktop", "/Documents")
	}
	patterns = append(patterns, "/Library/*")

	keep := newKeeper(in)
	kept := map[string][]string{}
	for _, p := range in.Library {
		parts := strings.Split(strings.Trim(strings.TrimPrefix(p, in.OldHome), "/"), "/")
		if parts[0] != "Library" || len(parts) < 2 || len(parts) > 3 ||
			!slices.Contains(sortedLibrary, parts[1]) && !slices.Contains(wholeLibrary, parts[1]) {
			continue
		}
		names := kept[parts[1]]
		if len(parts) == 3 && keep.entry(parts[1], parts[2]) {
			names = append(names, parts[2])
		}
		kept[parts[1]] = names
	}
	for _, dir := range wholeLibrary {
		if _, ok := kept[dir]; ok {
			patterns = append(patterns, "!/Library/"+dir)
		}
	}
	for _, dir := range sortedLibrary {
		names, ok := kept[dir]
		if !ok {
			continue
		}
		patterns = append(patterns, "!/Library/"+dir, "/Library/"+dir+"/*")
		slices.Sort(names)
		for _, name := range slices.Compact(names) {
			patterns = append(patterns, "!/Library/"+dir+"/"+escape(name))
		}
	}
	return append(patterns, floePaths...)
}

type keeper struct {
	ids, teams, present map[string]bool
}

func newKeeper(in RuleInput) keeper {
	k := keeper{ids: map[string]bool{}, teams: map[string]bool{}, present: map[string]bool{}}
	for _, a := range in.Apps {
		for _, id := range append([]string{a.BundleID}, a.Nested...) {
			if id != "" {
				k.ids[id] = true
			}
		}
		if a.Team != "" {
			k.teams[a.Team] = true
		}
	}
	for name := range in.Present {
		k.present[strings.ToLower(name)] = true
	}
	return k
}

// entry says whether Library/<dir>/<name> comes back.
func (k keeper) entry(dir, name string) bool {
	// A line break would cut the exclude file's line in two.
	if strings.HasPrefix(name, "com.apple.") || strings.HasPrefix(name, "group.com.apple.") ||
		uuidRe.MatchString(name) || strings.ContainsAny(name, "\n\r") {
		return false
	}
	switch dir {
	case "Application Support":
		return !k.present[strings.ToLower(name)] && !isAppleName(name)
	case "Containers":
		return k.ids[name]
	case "Group Containers":
		if id, ok := strings.CutPrefix(name, "group."); ok {
			return k.ids[id]
		}
		team, _, ok := strings.Cut(name, ".")
		return ok && k.teams[team]
	case "Preferences":
		id, ok := strings.CutSuffix(name, ".plist")
		return ok && k.ids[id]
	default: // LaunchAgents
		return strings.HasSuffix(name, ".plist")
	}
}

func isAppleName(name string) bool {
	lower := strings.ToLower(name)
	return slices.ContainsFunc(appleNames, func(a string) bool {
		if prefix, ok := strings.CutSuffix(a, "*"); ok {
			return strings.HasPrefix(lower, strings.ToLower(prefix))
		}
		return lower == strings.ToLower(a)
	})
}

// escape makes name match itself only in an exclude file line: restic expands
// $VARIABLES and trims each line before matching it as a glob.
func escape(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch r {
		case '*', '?', '[', '\\':
			b.WriteRune('\\')
			b.WriteRune(r)
		case '$':
			b.WriteString("[$]")
		default:
			b.WriteRune(r)
		}
	}
	s := b.String()
	if last, size := utf8.DecodeLastRuneInString(s); unicode.IsSpace(last) {
		return s[:len(s)-size] + "[" + string(last) + "]"
	}
	return s
}
