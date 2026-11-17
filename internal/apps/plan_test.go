package apps

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/Jszigeti/floe/internal/inventory"
)

const recipeBrewfile = `tap "cirruslabs/cli"
brew "jq"
cask "google-chrome"
mas "Keynote", id: 409183694
vscode "ms-python.python"
`

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPlanFromSplitsBrewfile(t *testing.T) {
	recipe := t.TempDir()
	writeFile(t, filepath.Join(recipe, "Brewfile"), recipeBrewfile, 0o644)
	writeFile(t, filepath.Join(recipe, "applications.txt"), ".DS_Store\nZed.app\n", 0o644)
	list := []inventory.App{
		{Name: "Google Chrome", Source: inventory.SourceCask, Cask: "google-chrome"},
		{Name: "Zed", Source: inventory.SourceCask, Cask: "zed"},
		{Name: "Zed copy", Source: inventory.SourceCask, Cask: "zed"},
		{Name: "Keynote", Source: inventory.SourceAppStore, StoreID: "409183694"},
		{Name: "Voicebox", Source: inventory.SourceBackup},
	}

	p, err := PlanFrom(recipe, list)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := readFile(t, p.Brewfile), "tap \"cirruslabs/cli\"\nbrew \"jq\"\ncask \"google-chrome\"\n"; got != want {
		t.Errorf("Brewfile = %q, want %q", got, want)
	}
	if got, want := readFile(t, p.VSCode), "vscode \"ms-python.python\"\n"; got != want {
		t.Errorf("VSCode = %q, want %q", got, want)
	}
	if !slices.Equal(p.Casks, []string{"zed"}) || !slices.Equal(p.StoreIDs, []string{"409183694"}) || !slices.Equal(p.Expected, []string{"Zed.app"}) {
		t.Errorf("casks = %q, store ids = %q, expected = %q, want [zed], [409183694] and [Zed.app]", p.Casks, p.StoreIDs, p.Expected)
	}
	if !slices.Equal(p.BrewfileCasks, []string{"google-chrome"}) {
		t.Errorf("Brewfile casks = %q, want %q", p.BrewfileCasks, []string{"google-chrome"})
	}
	if readFile(t, filepath.Join(recipe, "Brewfile")) != recipeBrewfile {
		t.Error("the recipe's own Brewfile must stay whole, a resumed run plans from it again")
	}
}

func TestPlanFromWithoutBrewfile(t *testing.T) {
	p, err := PlanFrom(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Brewfile != "" || p.VSCode != "" || p.Casks != nil || p.StoreIDs != nil {
		t.Fatalf("plan = %+v, want no Brewfile, no casks and no App Store id", p)
	}
}

func TestListedAppsMatchesCasksAndStoreIDs(t *testing.T) {
	recipe := t.TempDir()
	writeFile(t, filepath.Join(recipe, "applications.txt"), "Google Chrome.app\n.DS_Store\nZed.app\nKeynote Creator Studio.app\nWireGuard.app\nPython 3.12\n", 0o644)
	writeFile(t, filepath.Join(recipe, "mas.txt"), " 111  Keynote           (15.2)\n222  WireGuard  (1.0.16)\n333  CapCut  (9.7.1)\n", 0o644)

	got := ListedApps(recipe, inventory.CaskIndex{"Google Chrome.app": "google-chrome", "WireGuard.app": "wireguard"})

	// The App Store wins over a cask, as in the inventory; macOS 26 names Keynote's bundle after its suite.
	want := []inventory.App{
		{Name: "Google Chrome", Source: inventory.SourceCask, Cask: "google-chrome"},
		{Name: "Keynote Creator Studio", Source: inventory.SourceAppStore, StoreID: "111"},
		{Name: "WireGuard", Source: inventory.SourceAppStore, StoreID: "222"},
		{Name: "CapCut", Source: inventory.SourceAppStore, StoreID: "333"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListedApps = %+v, want %+v", got, want)
	}
}

func TestPlanNeedsAdminOnlyForWhatCallsSudo(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	recipe, brewDir := t.TempDir(), t.TempDir()
	formulae := filepath.Join(recipe, "Brewfile.formulae")
	writeFile(t, formulae, "tap \"supabase/tap\"\nbrew \"jq\"\n", 0o644)
	casks := filepath.Join(recipe, "Brewfile.casks")
	writeFile(t, casks, "brew \"jq\"\ncask \"zed\"\n", 0o644)
	if (Plan{}).NeedsAdmin(nil) {
		t.Fatal("nothing to install needs no password")
	}
	if !(Plan{Brewfile: formulae}).NeedsAdmin(nil) {
		t.Fatal("installing Homebrew needs the password")
	}
	installTool(t, brewDir, "brew")
	dirs := []string{brewDir}
	for _, tc := range []struct {
		name string
		plan Plan
		want bool
	}{
		{"formulae", Plan{Brewfile: formulae, VSCode: formulae}, false},
		{"brewfile cask", Plan{Brewfile: casks, BrewfileCasks: []string{"zed"}}, true},
		{"inventory cask", Plan{Casks: []string{"zed"}}, true},
		{"app store", Plan{StoreIDs: []string{"409183694"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.plan.NeedsAdmin(dirs); got != tc.want {
				t.Errorf("NeedsAdmin = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCaskLineAcceptsBothQuotes(t *testing.T) {
	for _, line := range []string{`cask "arc"`, `cask 'arc'`} {
		if m := caskLineRe.FindStringSubmatch(line); m == nil || m[1] != "arc" {
			t.Errorf("caskLineRe on %s = %q, want the token arc", line, m)
		}
	}
}
