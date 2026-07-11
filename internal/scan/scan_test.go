package scan

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExcluded(t *testing.T) {
	excludes := []string{"/home/x/.Trash", "node_modules"}
	cases := map[string]bool{
		"/home/x/.Trash":            true,
		"/home/x/.Trash/f":          true,
		"/home/x/proj/node_modules": true,
		"/home/x/proj/src/main.go":  false,
		"/home/x/.Trashcan":         false,
	}
	for p, want := range cases {
		if got := Excluded(p, excludes); got != want {
			t.Errorf("Excluded(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestEstimateNonexistentRootReturnsZeroNoError(t *testing.T) {
	// WalkDir feeds the stat error for the missing root to our callback, which
	// treats unreadable entries as skippable rather than fatal — so this is a
	// zero-result, no-error outcome, not an error return.
	root := filepath.Join(t.TempDir(), "does-not-exist")
	total, top, err := Estimate(root, nil)
	if err != nil {
		t.Fatalf("err = %v, want nil for a nonexistent root", err)
	}
	if total != 0 || len(top) != 0 {
		t.Fatalf("total = %d, top = %v, want zero result", total, top)
	}
}

func TestEstimateExcludesMatchingFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "keep.txt"), 50)
	writeFile(t, filepath.Join(root, "state.vscdb.backup"), 9999)

	total, _, err := Estimate(root, []string{"state.vscdb.backup"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 50 {
		t.Fatalf("total = %d, want 50 (excluded file must not count)", total)
	}
}

func TestEstimateSumsAndRanks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "big/data.bin"), 3000)
	writeFile(t, filepath.Join(root, "small/note.txt"), 100)
	writeFile(t, filepath.Join(root, "small/node_modules/dep.js"), 9999)
	writeFile(t, filepath.Join(root, "root.txt"), 50)

	total, top, err := Estimate(root, []string{"node_modules"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3150 {
		t.Fatalf("total = %d, want 3150 (excluded dirs must not count)", total)
	}
	if len(top) != 2 || filepath.Base(top[0].Path) != "big" || top[0].Bytes != 3000 {
		t.Fatalf("top = %+v", top)
	}
}
