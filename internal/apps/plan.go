package apps

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Jszigeti/floe/internal/inventory"
)

// caskLineRe reads the token of a Brewfile `cask "token"` line.
var caskLineRe = regexp.MustCompile(`^cask\s+["']([^"']+)["']`)

// Plan is every install of a new Mac, each tool run once.
type Plan struct {
	Brewfile      string   // path of the recipe's Brewfile without its mas and vscode lines, "" when it has none
	BrewfileCasks []string // cask tokens of Brewfile, the apps it installs
	VSCode        string   // path of a Brewfile holding only the vscode lines, run once the editor exists
	Casks         []string // inventory casks the Brewfile lacks
	StoreIDs      []string // App Store ids, empty when the App Store step was skipped
	Expected      []string // app file names the old Mac's /Applications held
}

// PlanFrom splits the recipe in recipeDir against the inventory list. brew bundle would install
// the Brewfile's mas lines itself, twice with StoreIDs, so they are dropped; the vscode lines
// fail until VS Code is installed, so they go last. The filtered Brewfiles are written beside
// the recipe's own, which stays whole for a resumed run.
func PlanFrom(recipeDir string, list []inventory.App) (Plan, error) {
	p := Plan{Expected: listedApps(filepath.Join(recipeDir, "applications.txt"))}
	data, err := os.ReadFile(filepath.Join(recipeDir, "Brewfile"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Plan{}, err
	}
	var main, vscode []string
	for _, line := range strings.Split(string(data), "\n") {
		switch kind(line) {
		case "", "mas":
		case "vscode":
			vscode = append(vscode, line)
		default:
			main = append(main, line)
			if m := caskLineRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
				p.BrewfileCasks = append(p.BrewfileCasks, path.Base(m[1]))
			}
		}
	}
	if p.Brewfile, err = writeLines(filepath.Join(recipeDir, "Brewfile.apps"), main); err != nil {
		return Plan{}, err
	}
	if p.VSCode, err = writeLines(filepath.Join(recipeDir, "Brewfile.vscode"), vscode); err != nil {
		return Plan{}, err
	}
	for _, a := range list {
		switch {
		case a.Source == inventory.SourceCask && a.Cask != "" && !slices.Contains(p.BrewfileCasks, a.Cask) && !slices.Contains(p.Casks, a.Cask):
			p.Casks = append(p.Casks, a.Cask)
		case a.Source == inventory.SourceAppStore && a.StoreID != "" && !slices.Contains(p.StoreIDs, a.StoreID):
			p.StoreIDs = append(p.StoreIDs, a.StoreID)
		}
	}
	return p, nil
}

// NeedsAdmin says whether a step of p calls sudo: the Homebrew installer, a cask, mas. Formulae
// and editor extensions install as the user.
func (p Plan) NeedsAdmin(brewDirs []string) bool {
	if len(p.Casks) > 0 || len(p.StoreIDs) > 0 {
		return true
	}
	if p.Brewfile == "" && p.VSCode == "" {
		return false
	}
	return FindTool("brew", brewDirs) == "" || len(p.BrewfileCasks) > 0
}

// ListedApps stands in for the inventory of a backup made before it existed: the
// applications.txt entries Homebrew has a cask for, and the mas.txt apps, named after their
// applications.txt entry so the summary knows which bundles they bring back.
func ListedApps(recipeDir string, casks inventory.CaskIndex) []inventory.App {
	files := listedApps(filepath.Join(recipeDir, "applications.txt"))
	var store []inventory.App
	fromStore := map[string]bool{}
	masList, _ := os.ReadFile(filepath.Join(recipeDir, "mas.txt")) // a backup without mas.txt has no App Store app
	for _, e := range inventory.ParseMasList(string(masList)) {
		name := e.Name
		if file := storeFile(files, e.Name); file != "" {
			fromStore[file], name = true, strings.TrimSuffix(file, ".app")
		}
		store = append(store, inventory.App{Name: name, Source: inventory.SourceAppStore, StoreID: e.ID})
	}
	var list []inventory.App
	for _, file := range files {
		if token, ok := casks[file]; ok && !fromStore[file] {
			list = append(list, inventory.App{Name: strings.TrimSuffix(file, ".app"), Source: inventory.SourceCask, Cask: token})
		}
	}
	return append(list, store...)
}

// storeFile is the bundle of the App Store app called name: its own name, or a longer one as
// macOS 26 gives Keynote ("Keynote Creator Studio.app"); "" when none.
func storeFile(files []string, name string) string {
	if slices.Contains(files, name+".app") {
		return name + ".app"
	}
	i := slices.IndexFunc(files, func(f string) bool { return strings.HasPrefix(f, name+" ") && strings.HasSuffix(f, ".app") })
	if i < 0 {
		return ""
	}
	return files[i]
}

// kind is a Brewfile line's first word, "" for a blank line or a comment.
func kind(line string) string {
	f := strings.Fields(line)
	if len(f) == 0 || strings.HasPrefix(f[0], "#") {
		return ""
	}
	return f[0]
}

// writeLines writes lines to file and returns its path, or "" without writing when there are none.
func writeLines(file string, lines []string) (string, error) {
	if len(lines) == 0 {
		return "", nil
	}
	return file, os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// listedApps reads applications.txt, hidden files aside.
func listedApps(file string) []string {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var names []string
	for _, name := range strings.Split(string(data), "\n") {
		if name != "" && !strings.HasPrefix(name, ".") {
			names = append(names, name)
		}
	}
	return names
}
