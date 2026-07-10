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
		"/home/x/.Trash":               true,
		"/home/x/.Trash/f":             true,
		"/home/x/proj/node_modules":    true,
		"/home/x/proj/src/main.go":     false,
		"/home/x/.Trashcan":            false,
	}
	for p, want := range cases {
		if got := Excluded(p, excludes); got != want {
			t.Errorf("Excluded(%q) = %v, want %v", p, got, want)
		}
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
