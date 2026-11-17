package replace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Jszigeti/floe/internal/apps"
	"github.com/Jszigeti/floe/internal/inventory"
	"github.com/Jszigeti/floe/internal/run"
	"github.com/Jszigeti/floe/internal/ui"
)

// ReportFile is where WriteReport writes, as the summary names it.
const ReportFile = "~/.local/state/floe/replace-report.txt"

const (
	// loginwindow's restart event needs no Automation permission, where System Events asks for one.
	restartCommand = `osascript -e 'tell app "loginwindow" to «event aevtrrst»'`
	// labelWidth keeps a label and its hint within 80 columns; longer name lists go under the label.
	labelWidth = 50
	// detailWidth is the room a name list has under its label.
	detailWidth = 73
)

// ReportPath is ReportFile in this home.
func ReportPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(ReportFile, "~/")), nil
}

// Action is one thing the run left to do.
type Action struct {
	Key     string // "1", "2"…, "R"
	Label   string
	Detail  string // names that did not fit in the label, "" when none
	Command string // what floe runs when the user picks it, "" when only the user can do it
}

// OldMac is what the replaced Mac's backup says beyond what the run did.
type OldMac struct {
	Apps          []inventory.App
	KeychainApps  []string // apps that kept a session in the old Mac's keychain
	UntrustedTaps []string
	// TurnOnDesktopDocuments: the old Mac kept Desktop and Documents in iCloud Drive, this one does not.
	TurnOnDesktopDocuments bool
}

// Actions lists what is left, one action per thing to do, Homebrew first since the other
// installs need it, then R to restart.
func Actions(res Result, old OldMac) []Action {
	var list []Action
	add := func(a Action) {
		a.Key = strconv.Itoa(len(list) + 1)
		list = append(list, a)
	}
	if len(old.KeychainApps) > 0 {
		add(named("Sign in again to %s", old.KeychainApps, plural(len(old.KeychainApps), "app", "apps")))
	}
	if old.TurnOnDesktopDocuments {
		add(Action{Label: "Turn on Desktop & Documents in iCloud Drive"})
	}
	var commands []string
	for _, c := range res.Apps.Commands {
		if !slices.Contains(commands, c) {
			commands = append(commands, c)
		}
	}
	homebrew := slices.Contains(commands, apps.HomebrewInstall)
	brewfile := slices.ContainsFunc(commands, func(c string) bool {
		file, ok := strings.CutPrefix(c, "brew bundle install --file ")
		return ok && !strings.HasSuffix(file, ".vscode")
	})
	if homebrew {
		add(Action{Label: "Install Homebrew (" + res.Apps.Homebrew + ")", Command: apps.HomebrewInstall})
	}
	// Trusting a tap only helps a Brewfile still to install: one that installed did without it.
	if homebrew || brewfile {
		for _, tap := range old.UntrustedTaps {
			add(Action{Label: "Trust the " + tap + " repository", Command: "brew trust " + tap})
		}
	}
	store := map[string]bool{}
	var retried []string // casks a left step installs by name
	for _, c := range commands {
		runs := func(a Action) {
			a.Command = c
			add(a)
		}
		file, bundle := strings.CutPrefix(c, "brew bundle install --file ")
		tokens, cask := strings.CutPrefix(c, "brew install --cask ")
		_, ids, mas := strings.Cut(c, "mas install ")
		switch {
		case bundle && strings.HasSuffix(file, ".vscode"):
			runs(Action{Label: "Install the VS Code extensions"})
		case bundle:
			runs(Action{Label: "Install your Homebrew apps and tools"})
		case cask:
			retried = append(retried, strings.Fields(tokens)...)
			names := appNames(old.Apps, strings.Fields(tokens), func(a inventory.App) string { return a.Cask })
			runs(named("Install %s with Homebrew", names, plural(len(names), "app", "apps")))
		case mas:
			for _, id := range strings.Fields(ids) {
				store[id] = true
			}
			names := appNames(old.Apps, strings.Fields(ids), func(a inventory.App) string { return a.StoreID })
			runs(named("Sign in to the App Store, then install %s", names, plural(len(names), "app", "apps")))
		}
	}
	// A Brewfile that failed without naming its failures may hold any cask.
	everyCask := homebrew || brewfile && !slices.ContainsFunc(res.Apps.BrewFailed, func(f string) bool { return !slices.Contains(retried, f) })
	covered := func(a inventory.App) bool {
		failed := slices.ContainsFunc(res.Apps.BrewFailed, func(f string) bool { return strings.EqualFold(f, a.Cask) || strings.EqualFold(f, a.Name) })
		return store[a.StoreID] || a.Source == inventory.SourceCask && (everyCask || failed)
	}
	if hand := byHand(res.Apps.Missing, old.Apps, covered); len(hand) > 0 {
		add(named("Install %s by hand", hand, plural(len(hand), "app", "apps")))
	}
	if n := len(res.Partial); n > 0 {
		add(Action{Label: fmt.Sprintf("Check the %s floe could not restore, in the full report", counted(n, "file", "files"))})
	}
	if n := len(res.SettingsErrs); n > 0 {
		add(Action{Label: fmt.Sprintf("Set again the %s floe could not carry, in the full report", counted(n, "Apple setting", "Apple settings"))})
	}
	return append(list, Action{Key: "R", Label: "Restart now", Command: restartCommand})
}

// byHand keeps the missing app bundles no action brings back.
func byHand(missing []string, list []inventory.App, covered func(inventory.App) bool) []string {
	var hand []string
	for _, file := range missing {
		name, ok := strings.CutSuffix(file, ".app")
		if !ok {
			continue
		}
		i := slices.IndexFunc(list, func(a inventory.App) bool { return a.Name == name })
		if i >= 0 && covered(list[i]) {
			continue
		}
		hand = append(hand, name)
	}
	return hand
}

// appNames names each key through the inventory app whose field matches it, the key itself when none.
func appNames(list []inventory.App, keys []string, field func(inventory.App) string) []string {
	names := make([]string, len(keys))
	for i, k := range keys {
		names[i] = k
		if j := slices.IndexFunc(list, func(a inventory.App) bool { return field(a) == k && a.Name != "" }); j >= 0 {
			names[i] = list[j].Name
		}
	}
	return names
}

// named fills format with the names when the label stays short, else with their count, the
// names going under it.
func named(format string, names []string, count string) Action {
	if label := fmt.Sprintf(format, and(names)); utf8.RuneCountInString(label) <= labelWidth {
		return Action{Label: label}
	}
	return Action{Label: fmt.Sprintf(format, count), Detail: strings.Join(names, ", ")}
}

func and(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// counted is plural for a phrase after "the": "the file", "the 2 files".
func counted(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return plural(n, one, many)
}

// WriteSummary prints the end of a run: what came back, then the numbered actions, each hinted
// as one floe does when offer says floe will ask for them. A run whose restore failed starts
// with the failure, and lists no action: the resume does them.
func WriteSummary(w io.Writer, res Result, actions []Action, offer bool) {
	var b strings.Builder
	if len(res.RestoreErrs) > 0 {
		b.WriteString("! Part of the backup was not restored:\n")
		for _, err := range res.RestoreErrs {
			first, _, _ := strings.Cut(err.Error(), "\n")
			fmt.Fprintf(&b, "  %s\n", first)
		}
		fmt.Fprintf(&b, "\nFull report: %s\n", ReportFile)
		_, _ = io.WriteString(w, b.String()) // a terminal failing this write cannot show the error either; ReportFile keeps it
		return
	}
	b.WriteString(ui.DoneLine(fmt.Sprintf("Restored %.1f GB of files and settings", float64(res.Restored)/1e9), res.RestoreTook) + "\n")
	if n := res.Apps.FromHomebrew + res.Apps.FromAppStore + res.FromBackup; n > 0 {
		b.WriteString(ui.DoneLine("Installed "+plural(n, "app", "apps"), res.AppsTook) + "\n")
		var from []string
		for _, s := range []struct {
			n    int
			from string
		}{{res.Apps.FromHomebrew, "Homebrew"}, {res.Apps.FromAppStore, "the App Store"}, {res.FromBackup, "your backup"}} {
			if s.n > 0 {
				from = append(from, fmt.Sprintf("%d from %s", s.n, s.from))
			}
		}
		fmt.Fprintf(&b, "  %s\n", strings.Join(from, ", "))
	}
	fmt.Fprintf(&b, "\nDone in %s.", ui.Duration(res.Elapsed))
	if len(actions) > 1 {
		b.WriteString(" Still to do:")
	}
	b.WriteString("\n\n")
	column := 0
	for _, a := range actions {
		if offer && hinted(a) {
			column = max(column, min(utf8.RuneCountInString(a.Label), labelWidth))
		}
	}
	for _, a := range actions {
		if offer && hinted(a) {
			fmt.Fprintf(&b, "  %-2s %-*s   type %s, floe does it\n", a.Key, column, a.Label, a.Key)
		} else {
			fmt.Fprintf(&b, "  %-2s %s\n", a.Key, a.Label)
		}
		for _, line := range wrap(a.Detail, detailWidth) {
			fmt.Fprintf(&b, "     %s\n", line)
		}
	}
	fmt.Fprintf(&b, "\nFull report: %s\n", ReportFile)
	_, _ = io.WriteString(w, b.String()) // a terminal failing this write cannot show the error either; ReportFile keeps it
}

// hinted says whether the line tells the user floe can do it; restarting needs no telling.
func hinted(a Action) bool {
	return a.Command != "" && a.Key != "R"
}

// wrap cuts a comma-separated list into lines of at most width runes, between names.
func wrap(list string, width int) []string {
	if list == "" {
		return nil
	}
	var lines []string
	line := ""
	for _, name := range strings.SplitAfter(list, ", ") {
		if line != "" && utf8.RuneCountInString(line+strings.TrimSpace(name)) > width {
			lines = append(lines, strings.TrimSpace(line))
			line = ""
		}
		line += name
	}
	return append(lines, line)
}

// WriteReport saves everything the run could not do, in full, with each action's command. A run
// whose restore failed lists no action: the resume does them.
func WriteReport(path string, res Result, actions []Action) error {
	var b strings.Builder
	b.WriteString("floe replace report\n")
	fmt.Fprintf(&b, "\nRestored %.1f GB of files and settings in %s, apps done in %s, run done in %s.\n",
		float64(res.Restored)/1e9, ui.Duration(res.RestoreTook), ui.Duration(res.AppsTook), ui.Duration(res.Elapsed))
	fmt.Fprintf(&b, "Installed %d from Homebrew, %d from the App Store, %d from the backup.\n", res.Apps.FromHomebrew, res.Apps.FromAppStore, res.FromBackup)
	section(&b, "Restore steps that failed", errorLines(res.RestoreErrs))
	section(&b, fmt.Sprintf("Files floe could not restore (%d)", len(res.Partial)), res.Partial)
	section(&b, "Homebrew could not install", res.Apps.BrewFailed)
	section(&b, "VS Code extensions that did not install", res.Apps.ExtensionsFailed)
	section(&b, "App Store apps that did not install", res.Apps.StoreFailed)
	section(&b, "Entries of /Applications not on this Mac", res.Apps.Missing)
	notes := slices.Clone(res.Apps.Notes)
	if res.Apps.Homebrew != "" {
		notes = append(notes, "Homebrew is not installed: "+res.Apps.Homebrew)
	}
	section(&b, "Notes", notes)
	section(&b, "Apple settings not carried", errorLines(res.SettingsErrs))
	if len(res.RestoreErrs) > 0 {
		b.WriteString("\nThe replace is not finished: run floe init again to resume.\n")
	} else {
		section(&b, "Still to do", todo(actions))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// todo is each action with its names and its command, for the report.
func todo(actions []Action) []string {
	var lines []string
	for _, a := range actions {
		lines = append(lines, a.Key+"  "+a.Label)
		if a.Detail != "" {
			lines = append(lines, "   "+a.Detail)
		}
		if a.Command != "" {
			lines = append(lines, "   "+a.Command)
		}
	}
	return lines
}

func section(b *strings.Builder, title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s:\n", title)
	for _, l := range lines {
		fmt.Fprintf(b, "  %s\n", l)
	}
}

// errorLines indents the lines after the first, so a multi-line error stays one entry.
func errorLines(errs []error) []string {
	lines := make([]string, len(errs))
	for i, err := range errs {
		lines[i] = strings.ReplaceAll(err.Error(), "\n", "\n  ")
	}
	return lines
}

// ErrNoHomebrew is Homebrew's installer leaving no brew, as it does when its download fails.
var ErrNoHomebrew = errors.New("homebrew is still not installed")

// RunAction runs what the user picked, on the terminal, since installers ask for the
// administrator password there. A shell opened before Homebrew lacks its tools and VS Code's code.
func RunAction(ctx context.Context, r run.Runner, brewDirs []string, a Action) error {
	path := strings.Join(append(slices.Clone(brewDirs), apps.VSCodeBin("/Applications"), os.Getenv("PATH")), ":")
	if _, err := r.Run(ctx, run.Cmd{Name: "/bin/sh", Args: []string{"-c", a.Command}, Env: []string{"PATH=" + path}, Terminal: true}); err != nil {
		return err
	}
	// The installer exits 0 when its own download fails.
	if a.Command == apps.HomebrewInstall && apps.FindTool("brew", brewDirs) == "" {
		return ErrNoHomebrew
	}
	return nil
}
