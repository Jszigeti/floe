package replace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/apps"
	"github.com/Jszigeti/floe/internal/inventory"
	"github.com/Jszigeti/floe/internal/restic"
	"github.com/Jszigeti/floe/internal/run"
)

// oldLibrary is the old home's Library as restic lists it.
var oldLibrary = []string{
	"/Users/alice/Library",
	"/Users/alice/Library/Application Support",
	"/Users/alice/Library/Application Support/Steam",
	"/Users/alice/Library/Application Support/Steam/userdata",
	"/Users/alice/Library/Application Support/com.apple.TCC",
	"/Users/alice/Library/Application Support/Already",
	"/Users/alice/Library/Preferences",
	"/Users/alice/Library/Preferences/com.google.Chrome.plist",
	"/Users/alice/Library/Preferences/com.leftover.plist",
	"/Users/alice/Library/Preferences/com.apple.dock.plist",
	"/Users/alice/Library/Preferences/ByHost",
	"/Users/alice/Library/Preferences/ByHost/.GlobalPreferences." + oldUUID + ".plist",
	"/Users/alice/Library/Preferences/ByHost/com.apple.Spotlight." + oldUUID + ".plist",
	"/Users/alice/Library/Preferences/ByHost/.GlobalPreferences.BBBBBBBB-1111-2222-3333-444444444444.plist",
}

// runFake answers a run: restic lists lib, restores write their exclude file into excludes by
// folder, and the preferences restore stages staged in its target. Its handler is safe for both tasks.
type runFake struct {
	*run.Fake
	mu       sync.Mutex
	excludes map[string]string
	staged   []string
}

func newRunFake(t *testing.T, lib []string) *runFake {
	t.Helper()
	f := &runFake{Fake: &run.Fake{}, excludes: map[string]string{}}
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name != "restic" {
			return "", nil
		}
		switch c.Args[0] {
		case "ls":
			for _, p := range lib {
				_, _ = fmt.Fprintf(c.Stdout, `{"path":%q,"type":"dir","struct_type":"node"}`+"\n", p)
			}
		case "restore":
			data, err := os.ReadFile(c.Args[len(c.Args)-1])
			if err != nil {
				t.Errorf("exclude file: %v", err)
			}
			target := c.Args[slices.Index(c.Args, "--target")+1]
			f.mu.Lock()
			f.excludes[c.Args[2]] = string(data)
			f.mu.Unlock()
			for _, name := range f.staged {
				if strings.HasSuffix(c.Args[2], "/Library/Preferences") {
					writeTestFile(t, filepath.Join(target, name), "<plist/>")
				}
			}
			_, _ = io.WriteString(c.Stdout, `{"message_type":"status","total_bytes":100,"bytes_restored":40}`+"\n"+
				`{"message_type":"summary","total_bytes":100,"bytes_restored":100}`+"\n")
		}
		return "", nil
	}
	return f
}

func (f *runFake) exclude(folder string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.excludes[folder]
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

func runDeps(t *testing.T, r run.Runner) RunDeps {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	return RunDeps{
		Client:   &restic.Client{Bin: "restic", Repo: "/repo", Pass: "pw", R: r},
		Runner:   r,
		Snapshot: "aaa111",
		Rule: RuleInput{OldHome: "/Users/alice", Apps: []inventory.App{
			{Name: "Google Chrome", BundleID: "com.google.Chrome", Source: inventory.SourceCask, Cask: "google-chrome"},
		}},
		NewHome: home,
		Install: apps.Deps{Runner: r, AppsRoot: filepath.Join(home, "Applications")},
		Log:     func(string, ...any) {},
	}
}

func TestRunRestoresTheHomeWithTheRule(t *testing.T) {
	f := newRunFake(t, oldLibrary)
	d := runDeps(t, f)
	// Present was read before an interrupted run restored Steam: Steam is the user's, Already macOS's.
	d.Rule.Present = map[string]bool{"Already": true}
	writeTestFile(t, filepath.Join(d.NewHome, "Library/Application Support/Steam/x"), "restored by the interrupted run")
	d.Status = &Status{}
	res, err := Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if !f.CalledWith("restic ls --recursive --json aaa111 /Users/alice/Library") ||
		!f.CalledWith("restore --json aaa111:/Users/alice --target "+d.NewHome) {
		t.Fatalf("calls = %+v", f.Calls)
	}
	home := f.exclude("aaa111:/Users/alice")
	for _, want := range []string{"\n!/Library/Application Support/Steam\n", "\n/Library/Preferences\n"} {
		if !strings.Contains(home, want) {
			t.Errorf("home exclude file lacks %q:\n%s", want, home)
		}
	}
	for _, never := range []string{"Already", "com.apple.TCC", "userdata"} {
		if strings.Contains(home, never) {
			t.Errorf("home exclude file lets %q in:\n%s", never, home)
		}
	}
	if !strings.HasSuffix(home, "\n/Library/Preferences\n") {
		t.Errorf("Preferences must stay out of the home restore, last line wins:\n%s", home)
	}
	if done, _ := d.Status.Read(); res.Restored != 100 || done.Done != 100 {
		t.Fatalf("restored %d, status %+v, want the summary's 100 bytes", res.Restored, done)
	}
}

func TestReplaceImportsThirdPartyPrefs(t *testing.T) {
	f := newRunFake(t, oldLibrary)
	byHost := "ByHost/.GlobalPreferences." + oldUUID + ".plist"
	f.staged = []string{"com.google.Chrome.plist", "com.apple.dock.plist", byHost}
	d := runDeps(t, f)
	if _, err := Run(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	staging := restoreTarget(f.Fake, "aaa111:/Users/alice/Library/Preferences")
	if staging == "" {
		t.Fatalf("preferences not staged; calls = %+v", f.Calls)
	}
	if strings.HasPrefix(staging, d.NewHome+"/") {
		t.Fatalf("preferences staged in the home, which the nightly backups read: %s", staging)
	}
	want := "/*\n!/com.google.Chrome.plist\n!/com.apple.dock.plist\n!/com.apple.finder.plist\n"
	prefs := f.exclude("aaa111:/Users/alice/Library/Preferences")
	if !strings.HasPrefix(prefs, want) || !strings.HasSuffix(prefs, "!/ByHost\n/ByHost/*\n!/"+byHost+"\n") || strings.Contains(prefs, "leftover") {
		t.Fatalf("preferences exclude file:\n%s", prefs)
	}
	if !f.CalledWith("defaults import com.google.Chrome " + filepath.Join(staging, "com.google.Chrome.plist")) {
		t.Fatal("Chrome's preferences not imported through defaults")
	}
	if f.CalledWith("defaults import com.apple.dock") {
		t.Fatal("an Apple domain imported whole")
	}
	// The old Mac named most ByHost files, so its CurrentHost domain is the one carried.
	if !f.CalledWith("plutil -convert xml1 -o - "+filepath.Join(staging, "com.apple.dock.plist")) ||
		!f.CalledWith("plutil -convert xml1 -o - "+filepath.Join(staging, byHost)) {
		t.Fatal("Apple settings not read from the staged plists")
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("staged preferences left behind: %v", err)
	}
}

func TestRunTakesTheOldMacFromItsConfig(t *testing.T) {
	f := newRunFake(t, oldLibrary)
	other := "BBBBBBBB-1111-2222-3333-444444444444"
	d := runDeps(t, f)
	d.OldMachine = other
	if _, err := Run(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	prefs := f.exclude("aaa111:/Users/alice/Library/Preferences")
	if !strings.Contains(prefs, "!/ByHost/.GlobalPreferences."+other+".plist\n") || strings.Contains(prefs, oldUUID) {
		t.Fatalf("the old config names the old Mac, not the ByHost vote:\n%s", prefs)
	}
}

func TestReplaceRunsRestoreAndAppsInParallel(t *testing.T) {
	restoring, installing := make(chan struct{}), make(chan struct{})
	var once sync.Once
	wait := func(other <-chan struct{}, what string) {
		select {
		case <-other:
		case <-time.After(5 * time.Second):
			t.Errorf("%s never ran beside the other task", what)
		}
	}
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		switch {
		case c.Name == "restic" && c.Args[0] == "restore":
			close(restoring)
			wait(installing, "the apps")
		case filepath.Base(c.Name) == "brew":
			once.Do(func() { close(installing) })
			wait(restoring, "the restore")
		}
		return "", nil
	}}
	d := runDeps(t, f)
	brewDir := t.TempDir()
	writeTestFile(t, filepath.Join(brewDir, "brew"), "")
	t.Setenv("PATH", brewDir)
	d.Install.BrewDirs = []string{brewDir}
	d.Plan = apps.Plan{Casks: []string{"google-chrome"}}
	res, err := Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.RestoreErrs) > 0 {
		t.Fatalf("restore errors: %v", res.RestoreErrs)
	}
}

func TestRunRestoresBackupAppsMovedToThisHome(t *testing.T) {
	f := newRunFake(t, nil)
	d := runDeps(t, f)
	writeTestFile(t, filepath.Join(d.NewHome, "Applications/Here.app/Contents/x"), "installed")
	d.Apps = restic.Snapshot{ShortID: "ccc333", Paths: []string{
		"/Applications/FloeTestVoicebox.app", "/Users/alice/Applications/Zaap.app", "/Users/alice/Applications/Here.app",
	}}
	if _, err := Run(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"restore --json ccc333:/Applications/FloeTestVoicebox.app --target /Applications/.FloeTestVoicebox.app.floe-restore",
		"restore --json ccc333:/Users/alice/Applications/Zaap.app --target " + filepath.Join(d.NewHome, "Applications/.Zaap.app.floe-restore"),
	} {
		if !f.CalledWith(want) {
			t.Errorf("no %q; calls = %+v", want, f.Calls)
		}
	}
	if f.CalledWith("ccc333:/Users/alice/Applications/Here.app") {
		t.Fatal("an app already on this Mac restored over")
	}
	if excludes := f.exclude("ccc333:/Users/alice/Applications/Zaap.app"); strings.TrimSpace(excludes) != "" {
		t.Fatalf("an app bundle restored without some of its files: %q", excludes)
	}
}

// partialError is the error of a restore that could not write item.
func partialError(t *testing.T, item string) error {
	t.Helper()
	_, err := run.ExecRunner{}.Run(context.Background(), run.Cmd{Name: "sh", Args: []string{"-c",
		`printf '{"message_type":"error","error":{"message":"denied"},"during":"restore","item":"` + item + `"}\n' >&2; exit 1`}})
	return err
}

// restoreTarget is the --target of the restore of snapshot:folder, empty when none ran.
func restoreTarget(f *run.Fake, from string) string {
	i := slices.IndexFunc(f.Calls, func(c run.Cmd) bool {
		return c.Name == "restic" && len(c.Args) > 4 && c.Args[0] == "restore" && c.Args[2] == from
	})
	if i < 0 {
		return ""
	}
	return f.Calls[i].Args[4]
}

func TestRunGoesOnPastFailures(t *testing.T) {
	partial := partialError(t, "/Library/Mail/x.emlx")
	f := &run.Fake{Handler: func(c run.Cmd) (string, error) {
		switch {
		case c.Name == "restic" && c.Args[0] == "ls":
			_, _ = io.WriteString(c.Stdout, `{"path":"/Users/alice/Library/Preferences","type":"dir","struct_type":"node"}`+"\n")
		case c.Name == "restic" && strings.HasSuffix(c.Args[2], "/Library/Preferences"):
			return "", errors.New("restic restore: exit status 1: Fatal: unable to open repository")
		case c.Name == "restic" && c.Args[0] == "restore":
			return "", partial
		}
		return "", nil
	}}
	d := runDeps(t, f)
	res, err := Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Partial, []string{filepath.Join(d.NewHome, "Library/Mail/x.emlx")}) {
		t.Fatalf("partial = %q, want the path on this Mac", res.Partial)
	}
	if len(res.RestoreErrs) != 1 || !strings.Contains(res.RestoreErrs[0].Error(), "/Users/alice/Library/Preferences") {
		t.Fatalf("restore errors = %v", res.RestoreErrs)
	}

	f.Calls = nil
	d.Apps = restic.Snapshot{ShortID: "ccc333", Paths: []string{"/Users/alice/Applications/Zaap.app"}}
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "restic" && c.Args[0] == "ls" {
			return "", errors.New("restic ls: Fatal: no such snapshot")
		}
		if c.Name == "restic" && c.Args[0] == "restore" {
			writeTestFile(t, filepath.Join(c.Args[4], "Contents/x"), "restored")
		}
		return "", nil
	}
	res, err = Run(context.Background(), d)
	if err != nil || len(res.RestoreErrs) != 1 || !strings.Contains(res.RestoreErrs[0].Error(), "listing") || f.CalledWith("aaa111:") {
		t.Fatalf("a failed listing must stop only the home restore: %+v, %v", res, err)
	}
	if !f.CalledWith("restore --json ccc333:/Users/alice/Applications/Zaap.app") {
		t.Fatalf("the app bundles need no listing; calls = %+v", f.Calls)
	}
}

func TestRunRecountsAppsTheRestoreBroughtBack(t *testing.T) {
	f := newRunFake(t, nil)
	d := runDeps(t, f)
	d.Plan = apps.Plan{Expected: []string{"Zaap.app", "Gone.app"}}
	d.Apps = restic.Snapshot{ShortID: "ccc333", Paths: []string{"/Users/alice/Applications/Zaap.app"}}
	base := f.Handler
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "restic" && c.Args[0] == "restore" && strings.HasSuffix(c.Args[2], "Zaap.app") {
			writeTestFile(t, filepath.Join(c.Args[4], "Contents/x"), "restored")
		}
		return base(c)
	}
	res, err := Run(context.Background(), d)
	if err != nil || !slices.Equal(res.Apps.Missing, []string{"Gone.app"}) {
		t.Fatalf("missing = %q, %v, want only the app nothing brought back", res.Apps.Missing, err)
	}
}

func TestRunMovesARestoredBundleIntoPlace(t *testing.T) {
	f := newRunFake(t, nil)
	d := runDeps(t, f)
	d.Apps = restic.Snapshot{ShortID: "ccc333", Paths: []string{"/Users/alice/Applications/Zaap.app", "/Users/alice/Applications/Pkg.app"}}
	dir := filepath.Join(d.NewHome, "Applications")
	// An interrupted run left a partial copy, which restic would skip as already restored.
	writeTestFile(t, filepath.Join(dir, ".Zaap.app.floe-restore/Contents/stale"), "partial")
	base := f.Handler
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name != "restic" || c.Args[0] != "restore" || !strings.HasPrefix(c.Args[2], "ccc333:") {
			return base(c)
		}
		writeTestFile(t, filepath.Join(c.Args[4], "Contents/x"), "from the backup")
		if strings.HasSuffix(c.Args[2], "Pkg.app") {
			// A pkg cask installed the app while restic restored it.
			writeTestFile(t, filepath.Join(dir, "Pkg.app/Contents/x"), "installed")
		}
		return "", nil
	}
	res, err := Run(context.Background(), d)
	if err != nil || len(res.RestoreErrs) > 0 {
		t.Fatalf("Run = %v, restore errors %v", err, res.RestoreErrs)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "Zaap.app/Contents/x")); string(data) != "from the backup" {
		t.Fatalf("Zaap.app not moved into place: %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "Zaap.app/Contents/stale")); !os.IsNotExist(err) {
		t.Fatalf("the interrupted run's copy was kept: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "Pkg.app/Contents/x")); string(data) != "installed" {
		t.Fatalf("the installer's copy lost to the backup's: %q", data)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Fatalf("Applications = %v, want only both apps", entries)
	}
	if res.FromBackup != 1 {
		t.Fatalf("from the backup = %d, want Zaap alone, the installer brought Pkg", res.FromBackup)
	}
	if res.RestoreTook <= 0 || res.AppsTook <= 0 || res.RestoreTook > res.Elapsed || res.AppsTook > res.Elapsed {
		t.Fatalf("restore took %v, apps %v, run %v: each task's time must be within the run's", res.RestoreTook, res.AppsTook, res.Elapsed)
	}
}

func TestRunLeavesAPartialBundleOutForALaterRun(t *testing.T) {
	f := newRunFake(t, nil)
	d := runDeps(t, f)
	d.Apps = restic.Snapshot{ShortID: "ccc333", Paths: []string{"/Users/alice/Applications/Zaap.app"}}
	dir := filepath.Join(d.NewHome, "Applications")
	failed := partialError(t, "/Contents/Resources/x.icns")
	base := f.Handler
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name != "restic" || c.Args[0] != "restore" || !strings.HasPrefix(c.Args[2], "ccc333:") {
			return base(c)
		}
		writeTestFile(t, filepath.Join(c.Args[4], "Contents/x"), "from the backup")
		return "", failed
	}
	res, err := Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("Applications = %v, want nothing: a resume restores the bundle again", entries)
	}
	if len(res.Partial) != 0 {
		t.Fatalf("partial = %q, files of a bundle never moved into place", res.Partial)
	}
	if len(res.RestoreErrs) != 1 || !strings.Contains(res.RestoreErrs[0].Error(), "/Users/alice/Applications/Zaap.app") ||
		!strings.Contains(res.RestoreErrs[0].Error(), "Contents/Resources/x.icns") {
		t.Fatalf("restore errors = %v, want the bundle and its failed file", res.RestoreErrs)
	}
}

func TestRunClearsEveryLeftoverOfItsBundles(t *testing.T) {
	f := newRunFake(t, nil)
	d := runDeps(t, f)
	d.Apps = restic.Snapshot{ShortID: "ccc333", Paths: []string{"/Users/alice/Applications/Here.app", "/Users/alice/Applications/Stuck.app"}}
	dir := filepath.Join(d.NewHome, "Applications")
	// Both apps were moved into place after Ctrl+C left their copies behind.
	writeTestFile(t, filepath.Join(dir, "Here.app/Contents/x"), "installed")
	writeTestFile(t, filepath.Join(dir, "Stuck.app/Contents/x"), "installed")
	writeTestFile(t, filepath.Join(dir, ".Here.app.floe-restore/Contents/x"), "partial")
	stuck := filepath.Join(dir, ".Stuck.app.floe-restore")
	writeTestFile(t, filepath.Join(stuck, "x"), "partial")
	if err := os.Chmod(stuck, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(stuck, 0o755) })
	res, err := Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".Here.app.floe-restore")); !os.IsNotExist(err) {
		t.Fatalf("a leftover beside an app in place was kept: %v", err)
	}
	if len(res.RestoreErrs) != 1 || !strings.Contains(res.RestoreErrs[0].Error(), stuck) {
		t.Fatalf("restore errors = %v, want the leftover that could not be removed", res.RestoreErrs)
	}
}

func TestRunLeavesTheAppsSnapshotBundlesOutOfTheHomeRestore(t *testing.T) {
	f := newRunFake(t, oldLibrary)
	d := runDeps(t, f)
	d.Apps = restic.Snapshot{ShortID: "ccc333", Paths: []string{
		"/Applications/Safari.app", "/Users/alice/Applications/Zaap.app", "/Users/alice/Applications/Odd [1]*.app",
	}}
	if _, err := Run(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	home := f.exclude("aaa111:/Users/alice")
	for _, want := range []string{"\n/Applications/Zaap.app\n", "\n/Applications/Odd \\[1]\\*.app\n"} {
		if !strings.Contains(home, want) {
			t.Errorf("home exclude file lacks %q, so the home's broken copy wins:\n%s", want, home)
		}
	}
	if strings.Contains(home, "Safari") {
		t.Errorf("a bundle outside the old home in the home exclude file:\n%s", home)
	}
}

func TestRunReportsPreferencesFailuresAsPathsOnThisMac(t *testing.T) {
	f := newRunFake(t, oldLibrary)
	d := runDeps(t, f)
	failed := partialError(t, "/com.google.Chrome.plist")
	base := f.Handler
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "restic" && c.Args[0] == "restore" && strings.HasSuffix(c.Args[2], "/Library/Preferences") {
			_, _ = base(c)
			return "", failed
		}
		return base(c)
	}
	res, err := Run(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(d.NewHome, "Library/Preferences/com.google.Chrome.plist"); !slices.Equal(res.Partial, []string{want}) {
		t.Fatalf("partial = %q, want %q", res.Partial, want)
	}
}

func TestStatusReadsBothTasks(t *testing.T) {
	var s Status
	s.setRestore(restic.RestoreStatus{Done: 3, Total: 9})
	s.setApps(apps.Progress{Done: 1, Total: 4, Current: "zed"})
	if r, a := s.Read(); r.Done != 3 || a.Current != "zed" {
		t.Fatalf("Read = %+v, %+v", r, a)
	}
}
