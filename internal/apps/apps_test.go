package apps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Jszigeti/floe/internal/run"
)

// testDeps hides the machine's own brew and mas behind an empty PATH and empty Homebrew folders.
func testDeps(t *testing.T, f *run.Fake) Deps {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	return Deps{Runner: f, BrewDirs: []string{t.TempDir()}, AppsRoot: t.TempDir()}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func installTool(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	writeFile(t, p, "#!/bin/sh\n", 0o755)
	return p
}

func fullPlan(t *testing.T) Plan {
	t.Helper()
	dir := t.TempDir()
	p := Plan{Brewfile: filepath.Join(dir, "Brewfile.apps"), VSCode: filepath.Join(dir, "Brewfile.vscode"), Casks: []string{"zed"}, StoreIDs: []string{"111"}}
	writeFile(t, p.Brewfile, "brew \"jq\"\n", 0o644)
	writeFile(t, p.VSCode, "vscode \"ms-python.python\"\n", 0o644)
	return p
}

func TestInstallRunsBrewThenCasksThenStoreThenVSCode(t *testing.T) {
	f := &run.Fake{}
	d := testDeps(t, f)
	installTool(t, d.BrewDirs[0], "brew")
	installTool(t, d.BrewDirs[0], "mas")
	p := fullPlan(t)

	rep := Install(context.Background(), d, p, nil)

	order := make([]string, len(f.Calls))
	for i, c := range f.Calls {
		order[i] = callLine(c)
		if c.Terminal {
			t.Errorf("%s must not take the terminal, the live screen owns it", order[i])
		}
	}
	want := []string{"brew bundle install --file " + p.Brewfile, "brew install --cask --adopt zed", "mas install 111", "brew bundle install --file " + p.VSCode}
	if !slices.Equal(order, want) {
		t.Fatalf("calls = %q, want %q", order, want)
	}
	if len(rep.Commands) != 0 || len(rep.BrewFailed) != 0 || len(rep.StoreFailed) != 0 {
		t.Fatalf("report = %+v", rep)
	}
}

func TestInstallInstallsHomebrewWithoutAsking(t *testing.T) {
	f := &run.Fake{}
	d := testDeps(t, f)
	d.Admin = &Admin{r: f, dir: "/private/admin"}
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "/bin/bash" {
			installTool(t, d.BrewDirs[0], "brew")
		}
		return "", nil
	}

	Install(context.Background(), d, Plan{Casks: []string{"zed"}}, nil)

	if len(f.Calls) != 2 || f.Calls[0].Name != "/bin/bash" || f.Calls[0].Args[1] != HomebrewInstall {
		t.Fatalf("the Homebrew installer must run first: %+v", f.Calls)
	}
	if !slices.Contains(f.Calls[0].Env, "NONINTERACTIVE=1") || f.Calls[0].Terminal {
		t.Fatalf("the installer must not ask anything: %+v", f.Calls[0])
	}
	if f.Calls[1].Name != filepath.Join(d.BrewDirs[0], "brew") {
		t.Fatalf("the casks must use the brew just installed: %+v", f.Calls[1])
	}
}

func TestInstallGivesTheAdminToEveryCommand(t *testing.T) {
	f := &run.Fake{}
	d := testDeps(t, f)
	d.Admin = &Admin{r: f, dir: "/private/admin"}
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == "/bin/bash" {
			installTool(t, d.BrewDirs[0], "brew")
			installTool(t, d.BrewDirs[0], "mas")
		}
		return "", nil
	}
	p := fullPlan(t)
	p.StoreIDs = []string{"111", "222"}

	Install(context.Background(), d, p, nil)

	want := []string{
		"bash -c " + HomebrewInstall,
		"brew bundle install --file " + p.Brewfile,
		"brew install --cask --adopt zed",
		"sudo -A -v", "mas install 111",
		"sudo -A -v", "mas install 222",
		"brew bundle install --file " + p.VSCode,
	}
	if got := callArgs(f); !slices.Equal(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	for _, c := range f.Calls {
		if !slices.Contains(c.Env, "SUDO_ASKPASS=/private/admin/askpass") {
			t.Errorf("%s must reach the password: %q", callLine(c), c.Env)
		}
	}
	if !slices.Contains(f.Calls[0].Env, "NONINTERACTIVE=1") {
		t.Errorf("the installer must stay non-interactive: %q", f.Calls[0].Env)
	}
}

// runnerErr is an error as run.ExecRunner words it: the command, the exit status, then stderr.
func runnerErr(c run.Cmd, stderr string) error {
	return fmt.Errorf("%s %v: %w: %s", c.Name, c.Args, errors.New("exit status 1"), stderr)
}

func TestInstallSkipsHomebrewWithoutAnAdmin(t *testing.T) {
	f := &run.Fake{}
	d := testDeps(t, f)
	p := fullPlan(t)
	p.Expected = []string{"Zed.app"}

	rep := Install(context.Background(), d, p, nil)

	if len(f.Calls) != 0 {
		t.Fatalf("the Homebrew installer needs an administrator, nothing can run: %+v", f.Calls)
	}
	want := []string{
		HomebrewInstall,
		"brew bundle install --file " + p.Brewfile,
		"brew install --cask zed",
		"brew install mas && mas install 111",
		"brew bundle install --file " + p.VSCode,
	}
	if !slices.Equal(rep.Commands, want) || !slices.Equal(rep.Missing, []string{"Zed.app"}) {
		t.Fatalf("report = %+v", rep)
	}
	if !slices.Equal(rep.Notes, []string{"Homebrew needs an administrator password"}) {
		t.Fatalf("notes = %q", rep.Notes)
	}
}

func TestInstallJudgesHomebrewByTheBrewItLeaves(t *testing.T) {
	cases := []struct {
		name      string
		leaveBrew bool
		stderr    string // empty: the installer exits 0
		notes     []string
	}{
		{name: "download failed, bash exits 0", notes: []string{"Homebrew did not install, check the internet connection"}},
		{name: "installer failed", stderr: "Need sudo access on macOS!\nmore", notes: []string{"Homebrew did not install: Need sudo access on macOS!"}},
		{name: "installer failed after installing brew", leaveBrew: true, stderr: "Warning: /opt/homebrew/bin is not in your PATH."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &run.Fake{}
			d := testDeps(t, f)
			d.Admin = &Admin{r: f, dir: "/private/admin"}
			f.Handler = func(c run.Cmd) (string, error) {
				if c.Name != "/bin/bash" {
					return "", nil
				}
				if tc.leaveBrew {
					installTool(t, d.BrewDirs[0], "brew")
				}
				if tc.stderr == "" {
					return "", nil
				}
				return "", runnerErr(c, tc.stderr)
			}

			rep := Install(context.Background(), d, Plan{Casks: []string{"zed"}}, nil)

			if tc.leaveBrew {
				if len(f.Calls) != 2 || len(rep.Commands) != 0 {
					t.Fatalf("brew is there, the casks must use it: calls %q, report %+v", callArgs(f), rep)
				}
			} else if !slices.Equal(rep.Commands, []string{HomebrewInstall, "brew install --cask zed"}) {
				t.Fatalf("commands = %q", rep.Commands)
			}
			if !slices.Equal(rep.Notes, tc.notes) {
				t.Fatalf("notes = %q, want %q", rep.Notes, tc.notes)
			}
		})
	}
}

func TestInstallKeepsWhyABrewBundleFailed(t *testing.T) {
	f := &run.Fake{}
	d := testDeps(t, f)
	installTool(t, d.BrewDirs[0], "brew")
	p := fullPlan(t)
	p.Casks, p.StoreIDs = nil, nil
	f.Handler = func(c run.Cmd) (string, error) {
		switch c.Args[3] {
		case p.Brewfile:
			return "", runnerErr(c, "Error: No such file or directory @ rb_sysopen\nbacktrace")
		case p.VSCode:
			return "", runnerErr(c, "Failed to install ms-python.python: no VS Code")
		}
		return "", nil
	}

	rep := Install(context.Background(), d, p, nil)

	if want := []string{"brew bundle failed on Brewfile.apps: Error: No such file or directory @ rb_sysopen"}; !slices.Equal(rep.Notes, want) {
		t.Fatalf("notes = %q, want %q", rep.Notes, want)
	}
	if !slices.Equal(rep.BrewFailed, []string{"ms-python.python"}) {
		t.Fatalf("failed = %q", rep.BrewFailed)
	}
}

func TestInstallKeepsTheTimestampFreshDuringADownload(t *testing.T) {
	old := refreshEvery
	refreshEvery = time.Millisecond
	t.Cleanup(func() { refreshEvery = old })
	f := &run.Fake{}
	d := testDeps(t, f)
	mas := installTool(t, d.BrewDirs[0], "mas")
	// The admin gets its own fake: its refreshes run beside the mas command.
	refreshed := make(chan struct{}, 1000)
	sudoRunner := &run.Fake{Handler: func(run.Cmd) (string, error) {
		select {
		case refreshed <- struct{}{}:
		default:
		}
		return "", nil
	}}
	d.Admin = &Admin{r: sudoRunner, dir: "/private/admin"}
	f.Handler = func(c run.Cmd) (string, error) {
		if c.Name == mas {
			// The refresh before the command, then at least one while it runs.
			for range 2 {
				select {
				case <-refreshed:
				case <-time.After(5 * time.Second):
					return "", errors.New("no refresh while mas downloads")
				}
			}
		}
		return "", nil
	}

	rep := Install(context.Background(), d, Plan{StoreIDs: []string{"111"}}, nil)

	if len(rep.StoreFailed) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	after := len(sudoRunner.Calls)
	time.Sleep(20 * time.Millisecond)
	if len(sudoRunner.Calls) != after {
		t.Fatal("the refreshes must stop with the mas command")
	}
}

func TestInstallParsesHomebrew7Failures(t *testing.T) {
	f := &run.Fake{}
	d := testDeps(t, f)
	brew := installTool(t, d.BrewDirs[0], "brew")
	mas := installTool(t, d.BrewDirs[0], "mas")
	p := fullPlan(t)
	p.Casks = []string{"zed", "arc"}
	p.StoreIDs = []string{"111", "222"}
	f.Handler = func(c run.Cmd) (string, error) {
		switch {
		case c.Name == brew && c.Args[0] == "bundle" && c.Args[3] == p.Brewfile:
			return "", fmt.Errorf("%s %v: exit status 1: Failed to install CapCut: download failed\nFailed to install jq: no bottle\nUpgrading git has failed!\n`brew bundle` failed! 3 Brewfile dependencies failed to install", c.Name, c.Args)
		case c.Name == brew && c.Args[0] == "install" && c.Args[3] == "arc":
			return "", errors.New("brew [install --cask arc]: exit status 1: Error: Cask 'arc' is unavailable")
		case c.Name == mas && c.Args[1] == "222":
			return "", errors.New("mas [install 222]: exit status 1: download failed")
		}
		return "", nil
	}

	rep := Install(context.Background(), d, p, nil)

	if !slices.Equal(rep.BrewFailed, []string{"CapCut", "jq", "git", "arc"}) || !slices.Equal(rep.StoreFailed, []string{"222"}) {
		t.Fatalf("failed = %q and %q", rep.BrewFailed, rep.StoreFailed)
	}
	want := []string{"brew bundle install --file " + p.Brewfile, "brew install --cask arc", "mas install 222"}
	if !slices.Equal(rep.Commands, want) {
		t.Fatalf("commands = %q, want %q", rep.Commands, want)
	}
}

// printing makes a fake tool print out on the stdout floe streams, as brew and mas do.
func printing(outs map[string]string) func(run.Cmd) (string, error) {
	return func(c run.Cmd) (string, error) {
		if out, ok := outs[callLine(c)]; ok && c.Stdout != nil {
			_, _ = c.Stdout.Write([]byte(out))
		}
		return "", nil
	}
}

func TestInstallReportsProgress(t *testing.T) {
	f := &run.Fake{}
	d := testDeps(t, f)
	installTool(t, d.BrewDirs[0], "brew")
	installTool(t, d.BrewDirs[0], "mas")
	p := fullPlan(t)
	writeFile(t, p.Brewfile, "tap \"a/b\"\n# tools\nbrew \"jq\"\nbrew \"git\"\nbrew \"wget\"\n\n", 0o644)
	f.Handler = printing(map[string]string{
		"brew bundle install --file " + p.Brewfile: "Using jq\nInstalling git\n==> Installing git dependency: pcre2\n==> Installing git\r######## 40%\rInstalling wget\n",
		"brew install --cask --adopt zed":          "==> Installing Cask zed\n",
		"mas install 111":                          "==> Installing Keynote (15.2)\n",
		"brew bundle install --file " + p.VSCode:   "Installing ms-python.python\n",
	})
	var seen []Progress
	Install(context.Background(), d, p, func(pr Progress) { seen = append(seen, pr) })

	if len(seen) == 0 {
		t.Fatal("no progress reported")
	}
	var currents []string
	for i, pr := range seen {
		if pr.Total != 6 {
			t.Fatalf("total = %d, want 3 brew + 1 cask + 1 store + 1 vscode", pr.Total)
		}
		if i > 0 && pr.Done < seen[i-1].Done {
			t.Fatalf("done went back: %+v", seen)
		}
		if !slices.Contains(currents, pr.Current) {
			currents = append(currents, pr.Current)
		}
	}
	if want := []string{"", "jq", "git", "wget", "zed", "111", "Keynote", "ms-python.python"}; !slices.Equal(currents, want) {
		t.Errorf("currents = %q, want %q", currents, want)
	}
	if last := seen[len(seen)-1]; last.Done != 6 {
		t.Errorf("last = %+v, want everything done", last)
	}
	if !slices.ContainsFunc(seen, func(pr Progress) bool { return pr.Current == "jq" && pr.Done == 1 }) {
		t.Errorf("a brew already there must count as done at once: %+v", seen)
	}
}

// Homebrew 7's bundle names every entry first, then installs them in one batch.
func TestInstallKeepsMovingThroughABatch(t *testing.T) {
	f := &run.Fake{}
	d := testDeps(t, f)
	installTool(t, d.BrewDirs[0], "brew")
	p := Plan{Brewfile: filepath.Join(t.TempDir(), "Brewfile.apps")}
	writeFile(t, p.Brewfile, "brew \"jq\"\nbrew \"git\"\ncask \"zed\"\n", 0o644)
	f.Handler = printing(map[string]string{
		"brew bundle install --file " + p.Brewfile: "Installing jq\nInstalling git\nInstalling zed\n" +
			"==> Pouring jq--1.7.1.arm64_sequoia.bottle.tar.gz\n==> Pouring git\n==> Installing Cask zed\n",
	})
	var seen []Progress
	Install(context.Background(), d, p, func(pr Progress) { seen = append(seen, pr) })

	var currents []string
	for i, pr := range seen {
		if pr.Done > pr.Total || (i > 0 && pr.Done < seen[i-1].Done) {
			t.Fatalf("count must stay in 0..%d and never go back: %+v", pr.Total, seen)
		}
		if len(currents) == 0 || currents[len(currents)-1] != pr.Current {
			currents = append(currents, pr.Current)
		}
	}
	if want := []string{"", "jq", "git", "zed", "jq", "git", "zed"}; !slices.Equal(currents, want) {
		t.Errorf("currents = %q, want %q", currents, want)
	}
	if !slices.ContainsFunc(seen, func(pr Progress) bool { return pr.Current == "git" && pr.Done == 2 }) {
		t.Errorf("the batch must not count an entry twice: %+v", seen)
	}
	if last := seen[len(seen)-1]; last.Done != 3 {
		t.Errorf("last = %+v, want everything done", last)
	}
}

func TestFindToolPrefersPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	want := installTool(t, dir, "brew")
	if got := findTool("brew", []string{t.TempDir()}); got != want {
		t.Fatalf("findTool = %q, want %q", got, want)
	}
}

func TestMasIDsKeepsNumericIDsOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mas.txt")
	writeFile(t, path, "111  Keynote  (15.2)\n\nNo installed apps found\n 222  Numbers  (15.2)\n", 0o644)
	if got := masIDs(path); !slices.Equal(got, []string{"111", "222"}) {
		t.Fatalf("masIDs = %q", got)
	}
}

func TestMissingAppsSkipsHiddenEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Safari.app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := missingApps([]string{"Safari.app", "Zed.app"}, root); !slices.Equal(got, []string{"Zed.app"}) {
		t.Fatalf("missingApps = %q", got)
	}
	list := filepath.Join(t.TempDir(), "applications.txt")
	writeFile(t, list, ".DS_Store\n.localized\nSafari.app\nZed.app\n", 0o644)
	if got := listedApps(list); !slices.Equal(got, []string{"Safari.app", "Zed.app"}) {
		t.Fatalf("listedApps = %q", got)
	}
	if got := listedApps(filepath.Join(root, "none.txt")); got != nil {
		t.Fatalf("no list must mean nothing expected, got %q", got)
	}
}
