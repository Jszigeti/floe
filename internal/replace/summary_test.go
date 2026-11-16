package replace

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/apps"
	"github.com/Jszigeti/floe/internal/inventory"
	"github.com/Jszigeti/floe/internal/run"
)

// trialApplications and trialMas are the recipe of the backup replaced on 2026-11-16, made by
// floe 0.1.5: no apps.json, so the inventory comes from these two lists.
const trialApplications = `.DS_Store
.localized
AnyDesk.app
CapCut.app
Capture d’écran 2026-10-16 à 23.23.07.png
Discord.app
Docker.app
Google Chrome.app
Keynote Creator Studio.app
Numbers Creator Studio.app
Pages Creator Studio.app
Python 3.12
Safari.app
Tempus Stopwatch.app
Utilities
Visual Studio Code.app
Voicebox.app
WireGuard.app
Zaap.app
logioptionsplus.app
`

const trialMas = `1500855883  CapCut            (9.7.1)
 361285480  Keynote           (15.2.1)
 361304891  Numbers           (15.2.1)
 361309726  Pages             (15.2.1)
1491326665  Tempus Stopwatch  (2026.2)
1451685025  WireGuard         (1.0.16)
`

// trialRun is what the run of 2026-11-16 left: the App Store signed out, the VS Code extensions
// failed, and ten entries of /Applications not back.
func trialRun(t *testing.T) (Result, OldMac) {
	t.Helper()
	recipe := t.TempDir()
	writeTestFile(t, filepath.Join(recipe, "applications.txt"), trialApplications)
	writeTestFile(t, filepath.Join(recipe, "mas.txt"), trialMas)
	list := apps.ListedApps(recipe, inventory.CaskIndex{"Google Chrome.app": "google-chrome", "Discord.app": "discord"})
	res := Result{
		Restored:    21_400_000_000,
		RestoreTook: 41 * time.Minute,
		AppsTook:    29 * time.Minute,
		Elapsed:     47 * time.Minute,
		Apps: apps.Report{
			StoreFailed: []string{"1500855883", "361285480", "361304891", "361309726", "1491326665", "1451685025"},
			Missing: []string{"Capture d’écran 2026-10-16 à 23.23.07.png", "Keynote Creator Studio.app", "Numbers Creator Studio.app",
				"Pages Creator Studio.app", "Python 3.12", "Tempus Stopwatch.app", "Voicebox.app", "WireGuard.app", "Zaap.app", "logioptionsplus.app"},
			Commands: []string{
				"mas install 1500855883 361285480 361304891 361309726 1491326665 1451685025",
				"brew bundle install --file /Users/admin/.local/state/floe/recipe/Brewfile.vscode",
			},
			FromHomebrew: 14,
		},
	}
	return res, OldMac{Apps: list}
}

func TestWriteSummaryMatchesSpec(t *testing.T) {
	res, old := trialRun(t)
	var out bytes.Buffer
	WriteSummary(&out, res, Actions(res, old), true)
	want := `✓ Restored 21.4 GB of files and settings            41m
✓ Installed 14 apps                                 29m
  14 from Homebrew

Done in 47m. Still to do:

  1  Sign in to the App Store, then install 6 apps   type 1, floe does it
     CapCut, Keynote Creator Studio, Numbers Creator Studio,
     Pages Creator Studio, Tempus Stopwatch, WireGuard
  2  Install the VS Code extensions                  type 2, floe does it
  3  Install Voicebox, Zaap and logioptionsplus by hand
  R  Restart now

Full report: ~/.local/state/floe/replace-report.txt
`
	if out.String() != want {
		t.Fatalf("summary:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestWriteSummaryCountsEverySource(t *testing.T) {
	res := Result{Restored: 1e9, RestoreTook: time.Minute, AppsTook: 2 * time.Minute, Elapsed: 3 * time.Minute, FromBackup: 3,
		Apps: apps.Report{FromHomebrew: 22, FromAppStore: 6}}
	var out bytes.Buffer
	WriteSummary(&out, res, Actions(res, OldMac{}), true)
	want := `✓ Restored 1.0 GB of files and settings             1m 0s
✓ Installed 31 apps                                 2m 0s
  22 from Homebrew, 6 from the App Store, 3 from your backup

Done in 3m 0s.

  R  Restart now

Full report: ~/.local/state/floe/replace-report.txt
`
	if out.String() != want {
		t.Fatalf("summary:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestWriteSummaryStartsWithTheFailure(t *testing.T) {
	res, old := trialRun(t)
	res.RestoreErrs = []error{
		errors.New("restoring /Users/alice: restic [restore]: exit status 1: Fatal: unable to open repository\nmore lines"),
		errors.New("/Applications/Zaap.app not restored, a later run retries it: restic could not write Contents/x"),
	}
	var out bytes.Buffer
	WriteSummary(&out, res, Actions(res, old), true)
	want := `! Part of the backup was not restored:
  restoring /Users/alice: restic [restore]: exit status 1: Fatal: unable to open repository
  /Applications/Zaap.app not restored, a later run retries it: restic could not write Contents/x

Full report: ~/.local/state/floe/replace-report.txt
`
	if out.String() != want {
		t.Fatalf("summary:\n%s\nwant:\n%s", out.String(), want)
	}
}

func labels(list []Action) []string {
	var got []string
	for _, a := range list {
		got = append(got, a.Key+" "+a.Label)
	}
	return got
}

// TestActionsWithoutHomebrew plays a run without the administrator password: Homebrew could
// not install, so everything that needs it waits for it.
func TestActionsWithoutHomebrew(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	recipe := t.TempDir()
	plan := apps.Plan{
		Brewfile: filepath.Join(recipe, "Brewfile.apps"), VSCode: filepath.Join(recipe, "Brewfile.vscode"),
		Casks: []string{"google-chrome"}, StoreIDs: []string{"409183694"},
		Expected: []string{"Google Chrome.app", "Keynote.app", "Zaap.app"},
	}
	writeTestFile(t, plan.Brewfile, "tap \"supabase/tap\"\nbrew \"jq\"\n")
	writeTestFile(t, plan.VSCode, "vscode \"ms-python.python\"\n")
	rep := apps.Install(context.Background(), apps.Deps{Runner: &run.Fake{}, BrewDirs: []string{t.TempDir()}, AppsRoot: t.TempDir()}, plan, nil)
	old := OldMac{
		Apps: []inventory.App{
			{Name: "Google Chrome", Source: inventory.SourceCask, Cask: "google-chrome"},
			{Name: "Keynote", Source: inventory.SourceAppStore, StoreID: "409183694"},
		},
		KeychainApps:           []string{"Chrome", "Discord", "Slack"},
		UntrustedTaps:          []string{"supabase/tap"},
		TurnOnDesktopDocuments: true,
	}

	got := Actions(Result{Apps: rep}, old)

	want := []string{
		"1 Sign in again to Chrome, Discord and Slack",
		"2 Turn on Desktop & Documents in iCloud Drive",
		"3 Install Homebrew (floe had no administrator password)",
		"4 Trust the supabase/tap repository",
		"5 Install your Homebrew apps and tools",
		"6 Install Google Chrome with Homebrew",
		"7 Sign in to the App Store, then install Keynote",
		"8 Install the VS Code extensions",
		"9 Install Zaap by hand",
		"R Restart now",
	}
	if !slices.Equal(labels(got), want) {
		t.Fatalf("actions:\n%s\nwant:\n%s", strings.Join(labels(got), "\n"), strings.Join(want, "\n"))
	}
	commands := []string{"", "", apps.HomebrewInstall, "brew trust supabase/tap", "brew bundle install --file " + plan.Brewfile,
		"brew install --cask google-chrome", "brew install mas && mas install 409183694", "brew bundle install --file " + plan.VSCode, "", restartCommand}
	for i, a := range got {
		if a.Command != commands[i] {
			t.Errorf("action %s runs %q, want %q", a.Key, a.Command, commands[i])
		}
	}
}

func TestActionsOnlyWhatIsLeft(t *testing.T) {
	res := Result{Apps: apps.Report{
		Missing:  []string{"Notes.txt", "Utilities", "Keynote.app", "Zed.app", "Pages.app"},
		Commands: []string{"mas install 1", "mas install 1"},
	}}
	old := OldMac{Apps: []inventory.App{
		{Name: "Keynote", Source: inventory.SourceAppStore, StoreID: "1"},
		// Pages's id installed fine, under another bundle name: only the user can find it.
		{Name: "Pages", Source: inventory.SourceAppStore, StoreID: "2"},
		// No Homebrew step is left, so nothing brings Zed back.
		{Name: "Zed", Source: inventory.SourceCask, Cask: "zed"},
	}}
	// The Brewfile installed, so the tap it needed was usable: nothing to trust.
	old.UntrustedTaps = []string{"supabase/tap"}
	res.Partial = []string{"/Users/bob/a", "/Users/bob/b"}
	res.SettingsErrs = []error{errors.New("com.apple.dock: x")}

	got := labels(Actions(res, old))

	want := []string{
		"1 Sign in to the App Store, then install Keynote",
		"2 Install Zed and Pages by hand",
		"3 Check the 2 files floe could not restore, in the full report",
		"4 Set again the Apple setting floe could not carry, in the full report",
		"R Restart now",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("actions:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestActionsKeepCaskAppsNoStepBringsBack(t *testing.T) {
	res := Result{Apps: apps.Report{
		BrewFailed: []string{"Arc"},
		Missing:    []string{"Arc.app", "Docker Desktop.app"},
		Commands:   []string{"brew bundle install --file /r/Brewfile.apps"},
	}}
	old := OldMac{
		Apps: []inventory.App{
			{Name: "Arc", Source: inventory.SourceCask, Cask: "arc"},
			// Its cask installed fine, under another bundle name than the old Mac's.
			{Name: "Docker Desktop", Source: inventory.SourceCask, Cask: "docker"},
		},
		UntrustedTaps: []string{"supabase/tap"},
	}

	got := labels(Actions(res, old))

	want := []string{
		"1 Trust the supabase/tap repository",
		"2 Install your Homebrew apps and tools",
		"3 Install Docker Desktop by hand",
		"R Restart now",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("actions:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	res.Apps.BrewFailed = nil
	if got := labels(Actions(res, old)); slices.Contains(got, "3 Install Docker Desktop by hand") {
		t.Fatalf("a Brewfile that failed without names may hold any cask: %q", got)
	}
}

func TestActionsLeaveTheExtensionsOutOfTheBrewfileCover(t *testing.T) {
	// The Brewfile failed without names, so it may hold any cask; one extension failed by name.
	res := Result{Apps: apps.Report{
		ExtensionsFailed: []string{"ms-python.python"},
		Notes:            []string{"brew bundle failed on Brewfile.apps: curl error"},
		Missing:          []string{"Arc.app"},
		Commands:         []string{"brew bundle install --file /r/Brewfile.apps", "brew bundle install --file /r/Brewfile.vscode"},
	}}
	old := OldMac{Apps: []inventory.App{{Name: "Arc", Source: inventory.SourceCask, Cask: "arc"}}}

	got := labels(Actions(res, old))

	want := []string{"1 Install your Homebrew apps and tools", "2 Install the VS Code extensions", "R Restart now"}
	if !slices.Equal(got, want) {
		t.Fatalf("actions:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestRunActionRestartAsks(t *testing.T) {
	list := Actions(Result{}, OldMac{})
	r := list[len(list)-1]
	if r.Key != "R" {
		t.Fatalf("last action = %+v, want the restart", r)
	}
	f := &run.Fake{}
	if err := RunAction(context.Background(), f, apps.BrewDirs, r); err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 1 || f.Calls[0].Name != "/bin/sh" || !slices.Equal(f.Calls[0].Args, []string{"-c", `osascript -e 'tell app "System Events" to restart'`}) {
		t.Fatalf("calls = %+v", f.Calls)
	}
	c := f.Calls[0]
	if !c.Terminal || len(c.Env) != 1 || !strings.HasPrefix(c.Env[0], "PATH=/opt/homebrew/bin:/usr/local/bin:") {
		t.Fatalf("a chosen action must own the terminal and find Homebrew's tools: %+v", c)
	}
}

func TestRunActionChecksHomebrewInstalled(t *testing.T) {
	// The installer exits 0 when its own download fails: only brew in place tells.
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	homebrew := Action{Key: "1", Label: "Install Homebrew", Command: apps.HomebrewInstall}
	if err := RunAction(context.Background(), &run.Fake{}, []string{dir}, homebrew); !errors.Is(err, ErrNoHomebrew) {
		t.Fatalf("err = %v, want ErrNoHomebrew", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "brew"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RunAction(context.Background(), &run.Fake{}, []string{dir}, homebrew); err != nil {
		t.Fatalf("brew is in place: %v", err)
	}
}

func TestReportHoldsEveryFailedPath(t *testing.T) {
	res, old := trialRun(t)
	res.Partial = []string{"/Users/bob/Library/Mail/x", "/Users/bob/Documents/locked.pdf"}
	res.SettingsErrs = []error{errors.New("com.apple.dock: defaults write failed")}
	res.Apps.BrewFailed = []string{"CapCut"}
	res.Apps.ExtensionsFailed = []string{"ms-python.python"}
	res.Apps.Notes = []string{"brew bundle failed on Brewfile.vscode: no VS Code"}
	res.Apps.Homebrew = "floe had no administrator password"
	path := filepath.Join(t.TempDir(), "state", "replace-report.txt")

	if err := WriteReport(path, res, Actions(res, old)); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	report := string(data)
	for _, want := range []string{
		"/Users/bob/Library/Mail/x", "/Users/bob/Documents/locked.pdf",
		"com.apple.dock: defaults write failed", "CapCut", "ms-python.python", "1451685025", "Python 3.12", "no VS Code", "floe had no administrator password",
		"mas install 1500855883 361285480", "Pages Creator Studio", `osascript -e 'tell app "System Events" to restart'`,
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q:\n%s", want, report)
		}
	}
}

func TestReportOfAFailedRestoreOnlySaysToResume(t *testing.T) {
	res, old := trialRun(t)
	res.RestoreErrs = []error{errors.New("restoring /Users/alice: Fatal\nsecond line")}
	path := filepath.Join(t.TempDir(), "replace-report.txt")

	if err := WriteReport(path, res, Actions(res, old)); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	report := string(data)
	if !strings.Contains(report, "restoring /Users/alice: Fatal\n  second line") ||
		!strings.Contains(report, "\nThe replace is not finished: run floe init again to resume.\n") {
		t.Fatalf("report lacks the failure or the resume:\n%s", report)
	}
	// The resume does them, and a restart now would only cut it.
	if strings.Contains(report, "Still to do") || strings.Contains(report, "restart") {
		t.Fatalf("a failed restore offers no action:\n%s", report)
	}
}

func TestWriteSummaryListsWithoutOfferingWhenFloeCannotRunThem(t *testing.T) {
	res, old := trialRun(t)
	var out bytes.Buffer
	WriteSummary(&out, res, Actions(res, old), false)
	if strings.Contains(out.String(), "floe does it") || !strings.Contains(out.String(), "  2  Install the VS Code extensions\n") {
		t.Fatalf("the list stays as a to-do, without hints:\n%s", out.String())
	}
}
