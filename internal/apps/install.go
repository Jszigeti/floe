package apps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Jszigeti/floe/internal/run"
)

// brewFailedRe matches a Brewfile entry that failed: "Failed to install <name>: …" since
// Homebrew 7, "Installing <name> has failed!" or "Upgrading <name> has failed!" otherwise.
var brewFailedRe = regexp.MustCompile(`Failed to install ([^:\n]+):|(?:Installing|Upgrading) (\S+) has failed!`)

// brewLineRe reads the entry brew works on: bundle's "Installing jq" and "Using jq", brew's
// "==> Installing jq", "==> Installing Cask zed" and "==> Pouring jq--1.7.1.arm64_sequoia.bottle.tar.gz".
// Dependency lines have more words.
var brewLineRe = regexp.MustCompile(`^(?:==> )?(Installing|Using|Pouring) (?:Cask )?(\S+?)(?:--\S+)?$`)

// masLineRe reads mas's "==> Installing Keynote (15.2)".
var masLineRe = regexp.MustCompile(`^==> Installing (.+?)(?: \([^)]*\))?$`)

// refreshEvery is how often sudo's timestamp is renewed while mas runs, well within its five minutes.
var refreshEvery = time.Minute

// masTimeout cuts an App Store install that hangs, as mas does when the App Store is signed out;
// it leaves room for Xcode's download on a slow line shared with the restore.
var masTimeout = 30 * time.Minute

// Install runs the plan in one pass: Homebrew when missing, the Brewfile, the casks, the App
// Store apps, the VS Code extensions. Nothing takes the terminal: the Homebrew installer runs
// with NONINTERACTIVE=1 and every sudo gets the password from d.Admin. A failing step
// never stops the next one; the Report says what is left. progress may be nil.
func Install(ctx context.Context, d Deps, p Plan, progress func(Progress)) Report {
	if progress == nil {
		progress = func(Progress) {}
	}
	t := &tracker{report: progress, total: entries(p.Brewfile) + len(p.Casks) + len(p.StoreIDs) + entries(p.VSCode)}
	t.emit()
	var rep Report
	brew := FindTool("brew", d.BrewDirs)
	if brew == "" && (p.Brewfile != "" || p.VSCode != "" || len(p.Casks) > 0) {
		brew = rep.homebrew(ctx, d, t)
	}
	rep.FromHomebrew, rep.BrewFailed = rep.bundle(ctx, d, t, brew, LeftBrewfile, p.Brewfile, p.BrewfileCasks)
	rep.casks(ctx, d, t, brew, p.Casks)
	rep.store(ctx, d, t, p.StoreIDs)
	_, rep.ExtensionsFailed = rep.bundle(ctx, d, t, brew, LeftExtensions, p.VSCode, nil, vscodeEnv(d.AppsRoot)...)
	rep.Missing = missingApps(p.Expected, d.AppsRoot)
	return rep
}

// homebrew runs Homebrew's installer and returns the brew it left, "" when none. Only brew
// itself tells: bash exits 0 when curl fails to download the installer. Without an Admin the
// installer is not run, since it stops at its first sudo.
func (rep *Report) homebrew(ctx context.Context, d Deps, t *tracker) string {
	if d.Admin == nil {
		rep.Leftovers = append(rep.Leftovers, Leftover{Kind: LeftHomebrew, Command: HomebrewInstall})
		rep.Homebrew = "floe had no administrator password"
		return ""
	}
	t.step(0, "Homebrew", "the Homebrew installer")
	installer := run.Cmd{Name: "/bin/bash", Args: []string{"-c", HomebrewInstall}, Env: append([]string{"NONINTERACTIVE=1"}, d.Admin.Env()...)}
	err := t.run(ctx, d.Runner, installer, brewLine)
	if brew := FindTool("brew", d.BrewDirs); brew != "" {
		return brew
	}
	rep.Leftovers = append(rep.Leftovers, Leftover{Kind: LeftHomebrew, Command: HomebrewInstall})
	// The installer exits 0 when its own download fails, so a missing brew with no error means no network.
	rep.Homebrew = "its download failed, check the internet connection"
	if err != nil {
		rep.Homebrew = "its installer failed: " + reason(err)
	}
	return ""
}

// bundle installs file's entries, left as kind when they fail, and returns how many of its casks, the apps, are in place, and
// the entries that failed: no cask in place when brew bundle failed without naming its failures.
func (rep *Report) bundle(ctx context.Context, d Deps, t *tracker, brew string, kind LeftoverKind, file string, casks []string, env ...string) (installed int, failed []string) {
	if file == "" {
		return 0, nil
	}
	t.step(entries(file), "", "brew")
	left := Leftover{Kind: kind, Command: "brew bundle install --file " + file}
	if brew == "" {
		rep.Leftovers = append(rep.Leftovers, left)
		t.finish()
		return 0, nil
	}
	c := run.Cmd{Name: brew, Args: []string{"bundle", "install", "--file", file}, Env: append(d.Admin.Env(), env...)}
	err := t.run(ctx, d.Runner, c, brewLine)
	if err == nil {
		return len(casks), nil
	}
	rep.Leftovers = append(rep.Leftovers, left)
	for _, m := range brewFailedRe.FindAllStringSubmatch(stderr(err), -1) {
		failed = append(failed, strings.TrimSpace(m[1]+m[2]))
	}
	if len(failed) == 0 {
		rep.Notes = append(rep.Notes, fmt.Sprintf("brew bundle failed on %s: %s", filepath.Base(file), reason(err)))
		return 0, nil
	}
	// Homebrew names a failed cask by its token or by its app's name, in any case. The clone
	// keeps the plan's own list whole.
	return len(slices.DeleteFunc(slices.Clone(casks), func(token string) bool {
		return slices.ContainsFunc(failed, func(f string) bool { return strings.EqualFold(f, token) })
	})), failed
}

// VSCodeBin is where VS Code keeps its code command, which installs its extensions.
func VSCodeBin(appsRoot string) string {
	return filepath.Join(appsRoot, "Visual Studio Code.app/Contents/Resources/app/bin")
}

// vscodeEnv puts VS Code's code on PATH when VS Code is in appsRoot: brew bundle installs the
// extensions with it, and its cask links it only in some cases, into a folder a shell opened
// before Homebrew lacks anyway.
func vscodeEnv(appsRoot string) []string {
	bin := VSCodeBin(appsRoot)
	if !fileExists(bin) {
		return nil
	}
	return []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")}
}

// stderr is what a failed command printed on stderr; an error no command returned is its own text.
func stderr(err error) string {
	var re *run.Error
	if errors.As(err, &re) {
		return re.Stderr
	}
	return err.Error()
}

// reason is the first line a failed command printed on stderr; its exit status when it printed nothing.
func reason(err error) string {
	msg := strings.TrimSpace(stderr(err))
	var re *run.Error
	if msg == "" && errors.As(err, &re) {
		msg = re.Err.Error()
	}
	line, _, _ := strings.Cut(msg, "\n")
	return line
}

// casks installs the inventory casks; --adopt takes over an app already in place instead of failing.
func (rep *Report) casks(ctx context.Context, d Deps, t *tracker, brew string, casks []string) {
	var left []string
	for _, c := range casks {
		t.step(1, c, "brew")
		if brew == "" {
			left = append(left, c)
			t.finish()
			continue
		}
		if err := t.run(ctx, d.Runner, run.Cmd{Name: brew, Args: []string{"install", "--cask", "--adopt", c}, Env: d.Admin.Env()}, brewLine); err != nil {
			rep.BrewFailed = append(rep.BrewFailed, c)
			left = append(left, c)
		} else {
			rep.FromHomebrew++
		}
	}
	if len(left) > 0 {
		rep.Leftovers = append(rep.Leftovers, Leftover{Kind: LeftCasks, Items: left, Command: "brew install --cask " + strings.Join(left, " ")})
	}
}

// store installs the App Store apps one by one. mas 7 gives no signed-out signal of its own,
// so a signed-out App Store shows as failed ids. mas calls sudo itself and relies on a fresh
// timestamp, renewed right before each id and all along its download, which can outlast it.
func (rep *Report) store(ctx context.Context, d Deps, t *tracker, ids []string) {
	if len(ids) == 0 {
		return
	}
	mas := FindTool("mas", d.BrewDirs)
	if mas == "" {
		t.step(len(ids), "", "mas")
		rep.Leftovers = append(rep.Leftovers, StoreLeftover(ids, false))
		t.finish()
		return
	}
	for _, id := range ids {
		t.step(1, id, "mas")
		stop := keepFresh(ctx, d.Admin)
		masCtx, cancel := context.WithTimeout(ctx, masTimeout)
		err := t.run(masCtx, d.Runner, run.Cmd{Name: mas, Args: []string{"install", id}, Env: d.Admin.Env()}, masLine)
		cancel()
		stop()
		if err != nil {
			rep.StoreFailed = append(rep.StoreFailed, id)
		} else {
			rep.FromAppStore++
		}
	}
	if len(rep.StoreFailed) > 0 {
		rep.Leftovers = append(rep.Leftovers, StoreLeftover(rep.StoreFailed, true))
	}
}

// keepFresh renews sudo's timestamp now, then every refreshEvery until stop, which returns once
// no renewal is left running. Nothing to do without an Admin.
func keepFresh(ctx context.Context, a *Admin) (stop func()) {
	if a == nil {
		return func() {}
	}
	// A failed renewal leaves mas to fail on its own, and the report then names the id.
	_ = a.Refresh(ctx)
	done, exited := make(chan struct{}), make(chan struct{})
	tick := time.NewTicker(refreshEvery)
	go func() {
		defer close(exited)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				_ = a.Refresh(ctx)
			}
		}
	}()
	return func() {
		close(done)
		<-exited
	}
}

// brewLine is the entry a brew line names, and whether it was already installed.
func brewLine(line string) (string, bool) {
	m := brewLineRe.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return m[2], m[1] == "Using"
}

func masLine(line string) (string, bool) {
	if m := masLineRe.FindStringSubmatch(line); m != nil {
		return m[1], false
	}
	return "", false
}

// tracker turns the tools' output into Progress. Each step owns a number of entries; an entry
// counts as done when the next one starts, and the step's end settles the count whatever the
// tool printed.
type tracker struct {
	report        func(Progress)
	total, done   int
	stepEnd       int
	current, tool string
	pending       bool
	seenThisStep  map[string]bool
}

func (t *tracker) emit() {
	at := 0
	if t.done < t.stepEnd {
		at = t.done + 1
	}
	t.report(Progress{Done: t.done, Total: t.total, At: at, Current: t.current, Tool: t.tool})
}

// step starts n entries run by tool; current names the first one, when known.
func (t *tracker) step(n int, current, tool string) {
	t.stepEnd, t.pending, t.seenThisStep = t.done+n, false, map[string]bool{}
	t.current, t.tool = current, tool
	t.emit()
}

func (t *tracker) finish() {
	t.done, t.pending = t.stepEnd, false
	t.emit()
}

// line is one line of output: a new entry moves the count, a named one the current entry, any
// line is a sign of life. Homebrew 7's bundle names every entry before installing them in one
// batch, so a name seen again only moves the current entry.
func (t *tracker) line(name string, using bool) {
	if t.seenThisStep[name] {
		t.current = name
	} else if name != "" {
		t.seenThisStep[name] = true
		if t.pending {
			t.advance()
		}
		t.current, t.pending = name, !using
		if using {
			t.advance()
		}
	}
	t.emit()
}

func (t *tracker) advance() {
	if t.done < t.stepEnd {
		t.done++
	}
}

// run streams c's output through parse, then settles the step.
func (t *tracker) run(ctx context.Context, r run.Runner, c run.Cmd, parse func(string) (string, bool)) error {
	w := &lineWriter{each: func(line string) { t.line(parse(strings.TrimSpace(line))) }}
	c.Stdout = w
	_, err := r.Run(ctx, c)
	w.flush()
	t.finish()
	return err
}

// lineWriter calls each for every line written to it. A carriage return ends a line too, so a
// download bar still shows the tool is alive.
type lineWriter struct {
	buf  []byte
	each func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexAny(w.buf, "\r\n")
		if i < 0 {
			return len(p), nil
		}
		if i > 0 {
			w.each(string(w.buf[:i]))
		}
		w.buf = w.buf[i+1:]
	}
}

func (w *lineWriter) flush() {
	if len(w.buf) > 0 {
		w.each(string(w.buf))
		w.buf = nil
	}
}
