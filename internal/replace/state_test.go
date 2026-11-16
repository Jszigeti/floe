package replace

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestStateSavedThenLoaded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	want := State{
		Backup: "MacBook-Pro", Repo: "/dest/Floe.backup/MacBook-Pro", SnapshotID: "aaa111",
		OldHome: "/Users/alice", Started: time.Date(2026, 11, 16, 9, 30, 0, 0, time.UTC),
		Present:        map[string]bool{"com.apple.TCC": true, "Already There": true},
		AppsSnapshotID: "ccc333", Restoring: true,
	}
	if err := want.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".local/state/floe/replace.toml")); err != nil {
		t.Fatalf("state not in floe's state folder: %v", err)
	}
	got, ok, err := LoadState()
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadState = %+v, %v, %v, want %+v", got, ok, err, want)
	}
}

func TestLoadStateWithoutAReplaceInProgress(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, ok, err := LoadState(); ok || err != nil {
		t.Fatalf("LoadState = %v, %v, want nothing to resume", ok, err)
	}
}

func TestLoadStateRefusesAnUnreadableFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(os.Getenv("HOME"), ".local/state/floe/replace.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("snapshot_id = ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadState(); err == nil {
		t.Fatal("a broken state file must not pass for no state")
	}
}

func TestClearStateForgetsTheReplace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := (State{Backup: "MacBook-Pro", SnapshotID: "aaa111"}).Save(); err != nil {
		t.Fatal(err)
	}
	if err := ClearState(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := LoadState(); ok {
		t.Fatal("state still there after ClearState")
	}
	if err := ClearState(); err != nil {
		t.Fatalf("clearing twice: %v", err)
	}
}

func TestStateNeedsAHome(t *testing.T) {
	t.Setenv("HOME", "")
	if err := (State{}).Save(); err == nil {
		t.Fatal("Save without a home")
	}
	if _, _, err := LoadState(); err == nil {
		t.Fatal("LoadState without a home")
	}
	if err := ClearState(); err == nil {
		t.Fatal("ClearState without a home")
	}
}
