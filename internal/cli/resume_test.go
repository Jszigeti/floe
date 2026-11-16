package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/replace"
	"github.com/Jszigeti/floe/internal/run"
)

func TestReplaceResumesPinnedSnapshot(t *testing.T) {
	_, dest, _ := startReplace(t)
	repo := fakeRepo(t, dest, "Old-Mac", 10*24*time.Hour)
	if err := (replace.State{Backup: "Old-Mac", Repo: repo, SnapshotID: "aaa111", OldHome: "/Users/alice"}).Save(); err != nil {
		t.Fatal(err)
	}
	f := replaceFake(nil, "1000") // its newest snapshot is bbb222
	out, err := runInit(f, "\npw\n")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Resume replacing Old-Mac? [Y/n]: ") || strings.Contains(out, "Backup destination") {
		t.Fatalf("want the resume offer instead of the wizard:\n%s", out)
	}
	c := resticCall(f, "restore")
	if c == nil || !slices.Contains(c.Args, "aaa111:/Users/alice") {
		t.Fatalf("restore call = %+v, want the pinned snapshot", c)
	}
	cfg, err := config.Load()
	if err != nil || cfg.Repo != repo || cfg.Destination != dest {
		t.Fatalf("config = %+v, %v", cfg, err)
	}
	if _, ok, _ := replace.LoadState(); ok {
		t.Fatal("state left behind by a finished replace")
	}
}

func TestReplaceKeepsItsStateUntilItEnds(t *testing.T) {
	home, dest, repo := startReplace(t)
	support := filepath.Join(home, "Library/Application Support")
	mkdirs(t, filepath.Join(support, "Already"))
	f := replaceFake(nil, "1000")
	base := f.Handler
	open := true
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "osascript" && slices.Contains(c.Args, "JavaScript") && open {
			return "com.tinyspeck.slackmacgap\tSlack\n", nil
		}
		return base(c)
	}
	if _, err := runInit(f, dest+"\n\npw\n"); err == nil {
		t.Fatal("want a stop while Slack is open")
	}
	st, ok, err := replace.LoadState()
	if err != nil || !ok || st.Backup != "MacBook-Air" || st.Repo != repo || st.SnapshotID != "bbb222" || st.OldHome != "/Users/alice" || st.Started.IsZero() {
		t.Fatalf("state = %+v, %v, %v, want the pinned snapshot", st, ok, err)
	}
	if !st.Present["Already"] {
		t.Fatalf("present = %v, want what macOS created before the run", st.Present)
	}
	open = false
	out, err := runInit(f, "y\npw\n")
	if err != nil || !strings.Contains(out, "Resume replacing MacBook-Air?") {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if _, ok, _ := replace.LoadState(); ok {
		t.Fatal("state left behind by a finished replace")
	}
}

func TestInitForgetsADeclinedResume(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	installFakeRestic(t)
	dest := filepath.Join(os.Getenv("HOME"), "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (replace.State{Backup: "Old-Mac", Repo: filepath.Join(dest, "Floe.backup/Old-Mac"), SnapshotID: "aaa111"}).Save(); err != nil {
		t.Fatal(err)
	}
	f := &run.Fake{Handler: freshInitHandler}
	out, err := runInit(f, "n\n"+dest+"\n\n\n")
	if err != nil || !strings.Contains(out, "Backup destination") {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if resticCall(f, "restore") != nil {
		t.Fatal("restored after the user declined")
	}
	if _, ok, _ := replace.LoadState(); ok {
		t.Fatal("a declined replace must be forgotten")
	}
}

func TestReplaceResumeReusesTheStoredPassphrase(t *testing.T) {
	_, dest, repo := startReplace(t)
	if err := (replace.State{Backup: "MacBook-Air", Repo: repo, SnapshotID: "aaa111", OldHome: "/Users/alice"}).Save(); err != nil {
		t.Fatal(err)
	}
	f := replaceFake(nil, "1000")
	base := f.Handler
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "security" && c.Args[0] == "find-generic-password" {
			return "pw\n", nil
		}
		return base(c)
	}
	out, err := runInit(f, "\n")
	if err != nil || strings.Contains(out, "Passphrase of") {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if cfg, err := config.Load(); err != nil || cfg.Destination != dest {
		t.Fatalf("config = %+v, %v", cfg, err)
	}
}

func TestReplaceResumeKeepsWhatMacOSHadCreated(t *testing.T) {
	home, _, repo := startReplace(t)
	if err := (replace.State{Backup: "MacBook-Air", Repo: repo, SnapshotID: "aaa111", OldHome: "/Users/alice",
		Present: map[string]bool{"Already": true}}).Save(); err != nil {
		t.Fatal(err)
	}
	// The interrupted run already restored Steam, beside what macOS had created.
	mkdirs(t, filepath.Join(home, "Library/Application Support/Steam"), filepath.Join(home, "Library/Application Support/Already"))
	f, excludes := listingFake("/Users/alice/Library/Application Support/Steam", "/Users/alice/Library/Application Support/Already")
	if out, err := runInit(f, "\npw\n"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	got := excludes["aaa111:/Users/alice"]
	if !strings.Contains(got, "\n!/Library/Application Support/Steam\n") || strings.Contains(got, "Already") {
		t.Fatalf("home exclude file, want Steam in and Already out:\n%s", got)
	}
}

func TestReplaceNamesTheOldMacFromItsConfig(t *testing.T) {
	_, dest, _ := startReplace(t)
	old, other := "BBBBBBBB-1111-2222-3333-444444444444", "CCCCCCCC-1111-2222-3333-444444444444"
	f, excludes := listingFake("/Users/alice/Library/Preferences",
		"/Users/alice/Library/Preferences/ByHost/.GlobalPreferences."+old+".plist",
		"/Users/alice/Library/Preferences/ByHost/.GlobalPreferences."+other+".plist",
		"/Users/alice/Library/Preferences/ByHost/com.apple.Spotlight."+other+".plist")
	base := f.Handler
	f.Handler = func(c run.Cmd) (string, error) {
		if len(c.Args) > 2 && c.Args[0] == "dump" && strings.HasSuffix(c.Args[2], "/config.toml") {
			return "machine = \"" + old + "\"\n" + oldMacConfig, nil
		}
		return base(c)
	}
	if out, err := runInit(f, dest+"\n\npw\n"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if got := excludes["bbb222:/Users/alice/Library/Preferences"]; !strings.Contains(got, "!/ByHost/.GlobalPreferences."+old+".plist\n") {
		t.Fatalf("preferences exclude file, want the old config's Mac:\n%s", got)
	}
}

// listingFake is replaceFake with lib under the old home's Library, keeping each restore's
// exclude file by restored folder.
func listingFake(lib ...string) (*run.Fake, map[string]string) {
	f := replaceFake(nil, "1000")
	base := f.Handler
	excludes := map[string]string{}
	f.Handler = func(c run.Cmd) (string, error) {
		switch {
		case len(c.Args) > 0 && c.Args[0] == "ls":
			for _, p := range lib {
				_, _ = fmt.Fprintf(c.Stdout, `{"path":%q,"type":"dir","struct_type":"node"}`+"\n", p)
			}
		case len(c.Args) > 2 && c.Args[0] == "restore":
			data, _ := os.ReadFile(c.Args[len(c.Args)-1])
			excludes[c.Args[2]] = string(data)
		}
		return base(c)
	}
	return f, excludes
}

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// appsFake is replaceFake whose backup lists snaps, and whose restores create their target.
func appsFake(snaps string) *run.Fake {
	f := replaceFake(nil, "1000")
	base := f.Handler
	f.Handler = func(c run.Cmd) (string, error) {
		switch {
		case len(c.Args) > 0 && c.Args[0] == "snapshots":
			return snaps, nil
		case len(c.Args) > 4 && c.Args[0] == "restore":
			_ = os.MkdirAll(c.Args[4], 0o755)
		}
		return base(c)
	}
	return f
}

const aliceApps = `{"time":"2026-09-18T13:00:00Z","short_id":"ccc333","paths":["/Users/alice/Applications/Zaap.app"],"tags":["apps"]}`

func TestReplacePinsTheAppsSnapshotWithTheHomeOne(t *testing.T) {
	_, dest, _ := startReplace(t)
	f := appsFake(`[{"time":"2026-09-18T13:00:00Z","short_id":"bbb222","paths":["/Users/alice"]},` + aliceApps + `]`)
	base := f.Handler
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "osascript" && slices.Contains(c.Args, "JavaScript") {
			return "com.tinyspeck.slackmacgap\tSlack\n", nil
		}
		return base(c)
	}
	if _, err := runInit(f, dest+"\n\npw\n"); err == nil {
		t.Fatal("want a stop while Slack is open")
	}
	if st, ok, err := replace.LoadState(); err != nil || !ok || st.AppsSnapshotID != "ccc333" {
		t.Fatalf("state = %+v, %v, %v, want the apps snapshot pinned", st, ok, err)
	}
}

func TestReplaceResumesPinnedAppsSnapshot(t *testing.T) {
	_, _, repo := startReplace(t)
	if err := (replace.State{Backup: "MacBook-Air", Repo: repo, SnapshotID: "bbb222", OldHome: "/Users/alice", AppsSnapshotID: "ccc333"}).Save(); err != nil {
		t.Fatal(err)
	}
	// A newer apps snapshot, as this Mac would make, must not replace the pinned one.
	f := appsFake(`[{"time":"2026-09-18T13:00:00Z","short_id":"bbb222","paths":["/Users/alice"]},` + aliceApps + `,` +
		`{"time":"2026-11-16T13:00:00Z","short_id":"eee555","paths":["/Users/alice/Applications/Other.app"],"tags":["apps"]}]`)
	if out, err := runInit(f, "\npw\n"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if !f.CalledWith("restore --json ccc333:/Users/alice/Applications/Zaap.app") || f.CalledWith("eee555") {
		t.Fatalf("calls = %+v, want the pinned apps snapshot restored", f.Calls)
	}
}

func TestReplaceResumeSkipsTheFreeSpaceCheckItPassed(t *testing.T) {
	_, _, repo := startReplace(t)
	if err := (replace.State{Backup: "MacBook-Air", Repo: repo, SnapshotID: "bbb222", OldHome: "/Users/alice", Restoring: true}).Save(); err != nil {
		t.Fatal(err)
	}
	f := replaceFake(nil, "1152921504606846976") // what the first run restored now fills the disk
	if out, err := runInit(f, "\npw\n"); err != nil || resticCall(f, "restore") == nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
}

func TestReplaceResumeChecksTheFreeSpaceBeforeTheRunStarted(t *testing.T) {
	_, _, repo := startReplace(t)
	if err := (replace.State{Backup: "MacBook-Air", Repo: repo, SnapshotID: "bbb222", OldHome: "/Users/alice"}).Save(); err != nil {
		t.Fatal(err)
	}
	f := replaceFake(nil, "1152921504606846976")
	if _, err := runInit(f, "\npw\n"); err == nil || !strings.Contains(err.Error(), "not enough free space") {
		t.Fatalf("err = %v", err)
	}
}

func TestInitOffersTheBackupAgainAfterADeclinedUnfinishedReplace(t *testing.T) {
	_, dest, _ := startReplace(t)
	if _, err := runInit(replaceFake(errors.New("Fatal: unable to open repository"), "1000"), dest+"\n\npw\n"); err == nil {
		t.Fatal("want the failed restore to leave the replace unfinished")
	}
	// No config of the unfinished replace may pass the old Mac's backup for this Mac's own.
	f := replaceFake(nil, "1000")
	out, err := runInit(f, "n\n"+dest+"\n\n")
	if err == nil || !strings.Contains(out, "Found a backup: MacBook-Air") || f.CalledWith("launchctl", "bootstrap") {
		t.Fatalf("init: %v\n%s", err, out)
	}
}
