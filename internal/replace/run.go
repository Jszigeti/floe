package replace

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Jszigeti/floe/internal/apps"
	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/restic"
	"github.com/Jszigeti/floe/internal/run"
)

// Status is how far both tasks of a run are: they write it from their goroutines, the screen reads it.
type Status struct {
	mu   sync.Mutex
	seen Progress
}

// Progress is what the screen shows of a run.
type Progress struct {
	Restore   restic.RestoreStatus // the home's restore
	Apps      apps.Progress
	AppsLines int64 // apps reports so far, one per line brew or mas printed: the stall check counts them
	// HomeDone says the home's files are in. The preferences, then the app bundles of the apps
	// snapshot follow, which report no status: Bundle numbers the one at work, 1..Bundles.
	HomeDone        bool
	Bundle, Bundles int
	// RestoreEnded and AppsEnded say a task is over, the restore's bundles and preferences included.
	RestoreEnded, AppsEnded bool
}

func (s *Status) Read() Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen
}

func (s *Status) update(f func(*Progress)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(&s.seen)
}

func (s *Status) setRestore(st restic.RestoreStatus) {
	s.update(func(p *Progress) { p.Restore = st })
}

func (s *Status) setApps(a apps.Progress) {
	s.update(func(p *Progress) { p.Apps, p.AppsLines = a, p.AppsLines+1 })
}

// RunDeps is what a run restores and installs, once every question was asked.
type RunDeps struct {
	Client   *restic.Client
	Runner   run.Runner
	Snapshot string          // the pinned home snapshot
	Apps     restic.Snapshot // the newest apps snapshot, zero when the backup has none
	// Rule holds the old home, the inventory, the iCloud flags and Present, read before any
	// restore or install wrote to the new home; Run lists the Library itself.
	Rule    RuleInput
	NewHome string
	// OldMachine is the old Mac's hardware UUID from its floe config, empty when it had none.
	OldMachine string
	Plan       apps.Plan
	Install    apps.Deps
	Status     *Status // nil when nothing reads it
}

// Result is what a run did and what it left to do.
type Result struct {
	Restored     int64    // bytes of the home restored or already in place
	Partial      []string // files restic could not write, as paths on this Mac
	RestoreErrs  []error  // restore steps that failed as a whole
	Apps         apps.Report
	FromBackup   int // app bundles the apps snapshot brought back
	Carried      int // Apple settings carried
	SettingsErrs []error
	// RestoreTook and AppsTook time each task, Elapsed the whole run.
	RestoreTook, AppsTook, Elapsed time.Duration
}

// Run restores the home and installs the apps side by side, then carries the Apple settings,
// which go last so no install resets them. A failing step never stops the others: it lands in
// the Result.
func Run(ctx context.Context, d RunDeps) (Result, error) {
	start := time.Now()
	tmp, err := os.MkdirTemp("", "floe-replace-")
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if d.Status == nil {
		d.Status = &Status{}
	}
	var res Result
	var out restoreTask
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		out = restoreAll(ctx, d, tmp)
		res.RestoreTook = time.Since(start)
		d.Status.update(func(p *Progress) { p.RestoreEnded = true })
	}()
	go func() {
		defer wg.Done()
		res.Apps = apps.Install(ctx, d.Install, d.Plan, d.Status.setApps)
		res.AppsTook = time.Since(start)
		d.Status.update(func(p *Progress) { p.AppsEnded = true })
	}()
	wg.Wait()
	res.Restored, res.Partial, res.RestoreErrs, res.FromBackup = out.bytes, out.partial, out.errs, out.bundles
	// Install counted the apps before the restore brought back the bundles no installer has.
	res.Apps.Missing = slices.DeleteFunc(res.Apps.Missing, func(name string) bool { return exists(filepath.Join(d.Install.AppsRoot, name)) })
	res.Carried, res.SettingsErrs = CarrySettings(ctx, d.Runner, out.settings, d.Rule.OldHome, d.NewHome, out.uuid)
	res.Elapsed = time.Since(start)
	return res, nil
}

// restoreTask is what the restore task did, and what CarrySettings needs from it.
type restoreTask struct {
	bytes    int64
	partial  []string
	errs     []error
	settings map[Domain]string // staged plist of each Apple domain the old Mac had
	uuid     string            // the old Mac's hardware UUID
	bundles  int               // app bundles moved into place
}

// restoreAll restores the home, then imports the apps' preferences, then the app bundles.
func restoreAll(ctx context.Context, d RunDeps, tmp string) restoreTask {
	var out restoreTask
	out.restoreHome(ctx, d, tmp)
	rehome := config.Rehomer(d.Rule.OldHome, d.NewHome)
	for i, p := range d.Apps.Paths {
		d.Status.update(func(pr *Progress) { pr.HomeDone, pr.Bundle, pr.Bundles = true, i+1, len(d.Apps.Paths) })
		target := rehome(p)
		staged := filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".floe-restore")
		// An interrupted run's copy: restic would skip its files as restored.
		if err := os.RemoveAll(staged); err != nil {
			out.errs = append(out.errs, fmt.Errorf("removing what an interrupted restore left of %s: %w", p, err))
			continue
		}
		// An app already on this Mac is the user's own install.
		if !exists(target) {
			out.restoreBundle(ctx, d, tmp, p, staged, target)
		}
	}
	return out
}

func (out *restoreTask) restoreHome(ctx context.Context, d RunDeps, tmp string) {
	lib, uuid, err := listLibrary(ctx, d.Client, d.Snapshot, d.Rule.OldHome)
	if err != nil {
		// Without the listing the rule cannot tell the user's data from the old Mac's own.
		out.errs = append(out.errs, fmt.Errorf("listing the backup's Library, home not restored: %w", err))
		return
	}
	out.uuid = cmp.Or(d.OldMachine, uuid)
	in := d.Rule
	in.Library = lib
	patterns := ExcludePatterns(in)
	// The apps snapshot's copy of these bundles is whole, the home's lost what its excludes match.
	for _, p := range d.Apps.Paths {
		if rel, ok := strings.CutPrefix(p, in.OldHome+"/"); ok {
			patterns = append(patterns, "/"+escape(rel))
		}
	}
	// Preferences go through defaults import, which cfprefsd sees, never as files under its feet.
	patterns = append(patterns, "/Library/Preferences")
	failed, _ := out.restore(ctx, d, tmp, d.Snapshot, in.OldHome, d.NewHome, patterns, d.Status.setRestore)
	d.Status.update(func(p *Progress) { p.HomeDone = true })
	out.failed(d.NewHome, failed)
	out.bytes = d.Status.Read().Restore.Done
	out.settings = out.importPrefs(ctx, d, tmp, in)
}

// restoreBundle restores the app bundle folder into staged, beside target, then moves it in
// whole, so an interrupted or partial restore never leaves a bundle that passes for installed
// and that a later run would skip.
func (out *restoreTask) restoreBundle(ctx context.Context, d RunDeps, tmp, folder, staged, target string) {
	defer func() { _ = os.RemoveAll(staged) }() // a failed removal is retried by the next run
	failed, ok := out.restore(ctx, d, tmp, d.Apps.ShortID, folder, staged, nil, func(restic.RestoreStatus) {})
	if len(failed) > 0 {
		out.errs = append(out.errs, fmt.Errorf("%s not restored, a later run retries it: restic could not write %s", folder, strings.Join(failed, ", ")))
		return
	}
	// Failed, or a pkg installer or the user put the app there meanwhile: their copy wins.
	if !ok || exists(target) {
		return
	}
	if err := os.Rename(staged, target); err != nil {
		out.errs = append(out.errs, fmt.Errorf("moving %s into place: %w", folder, err))
		return
	}
	out.bundles++
}

// failed records files, relative to dir, that restic could not write.
func (out *restoreTask) failed(dir string, files []string) {
	for _, f := range files {
		out.partial = append(out.partial, filepath.Join(dir, f))
	}
}

// restore restores folder of snapshot id into target, leaving out patterns. It returns the files
// restic could not write, relative to target, and false when the restore failed as a whole.
func (out *restoreTask) restore(ctx context.Context, d RunDeps, tmp, id, folder, target string, patterns []string, status func(restic.RestoreStatus)) ([]string, bool) {
	file, err := os.CreateTemp(tmp, "excludes-*")
	if err == nil {
		_, err = file.WriteString(strings.Join(patterns, "\n") + "\n")
		err = errors.Join(err, file.Close())
	}
	if err == nil {
		err = d.Client.RestoreFolder(ctx, id, folder, target, file.Name(), status)
	}
	var partial *restic.PartialRestoreError
	if errors.As(err, &partial) {
		return partial.Failed, true
	}
	if err != nil {
		out.errs = append(out.errs, fmt.Errorf("restoring %s: %w", folder, err))
		return nil, false
	}
	return nil, true
}

// importPrefs stages the inventoried apps' preferences and the Apple domains' in tmp, outside
// the home the nightly backups read, imports the apps' ones with defaults, and returns the Apple ones for CarrySettings.
func (out *restoreTask) importPrefs(ctx context.Context, d RunDeps, tmp string, in RuleInput) map[Domain]string {
	folder := in.OldHome + "/Library/Preferences"
	if !slices.Contains(in.Library, folder) {
		return nil
	}
	keep := newKeeper(in)
	var ids []string
	patterns := []string{"/*"}
	for _, p := range in.Library {
		if name, ok := strings.CutPrefix(p, folder+"/"); ok && keep.entry("Preferences", name) {
			ids = append(ids, strings.TrimSuffix(name, ".plist"))
			patterns = append(patterns, "!/"+escape(name))
		}
	}
	var byHost []string
	for _, dom := range appleDomains {
		switch {
		case !dom.CurrentHost:
			patterns = append(patterns, "!/"+escape(prefsFile(dom, "")))
		case out.uuid != "":
			byHost = append(byHost, "!/"+escape(prefsFile(dom, out.uuid)))
		}
	}
	if len(byHost) > 0 {
		patterns = append(append(patterns, "!/ByHost", "/ByHost/*"), byHost...)
	}
	staging := filepath.Join(tmp, "prefs")
	failed, ok := out.restore(ctx, d, tmp, d.Snapshot, folder, staging, patterns, func(restic.RestoreStatus) {})
	// The staging folder goes away with the run, so the files are named where they belong on this Mac.
	out.failed(filepath.Join(d.NewHome, "Library/Preferences"), failed)
	if !ok {
		return nil
	}
	for _, id := range ids {
		file := filepath.Join(staging, id+".plist")
		if !exists(file) {
			continue
		}
		if _, err := d.Runner.Run(ctx, run.Cmd{Name: "defaults", Args: []string{"import", id, file}}); err != nil {
			out.errs = append(out.errs, fmt.Errorf("importing the preferences of %s: %w", id, err))
		}
	}
	plists := map[Domain]string{}
	for _, dom := range appleDomains {
		if file := filepath.Join(staging, prefsFile(dom, out.uuid)); (!dom.CurrentHost || out.uuid != "") && exists(file) {
			plists[dom] = file
		}
	}
	return plists
}

// prefsFile is the file of dom under Preferences; a CurrentHost domain's is named after the Mac.
func prefsFile(dom Domain, uuid string) string {
	name := dom.Name
	if name == "NSGlobalDomain" {
		name = ".GlobalPreferences"
	}
	if dom.CurrentHost {
		return "ByHost/" + name + "." + uuid + ".plist"
	}
	return name + ".plist"
}

// listLibrary lists the old home's Library as deep as the rule reads it, and the hardware UUID
// of the old Mac: the one most ByHost files are named after, since a home moved from Mac to Mac
// keeps the older Macs' files too.
func listLibrary(ctx context.Context, c *restic.Client, id, oldHome string) ([]string, string, error) {
	var lib []string
	uuids := map[string]int{}
	err := c.List(ctx, id, oldHome+"/Library", func(e restic.Entry) {
		rel := strings.TrimPrefix(e.Path, oldHome+"/")
		if name, ok := strings.CutPrefix(rel, "Library/Preferences/ByHost/"); ok {
			if u := uuidRe.FindString(name); u != "" {
				uuids[u]++
			}
			return
		}
		if strings.Count(rel, "/") <= 2 {
			lib = append(lib, e.Path)
		}
	})
	uuid := ""
	for u, n := range uuids {
		if n > uuids[uuid] || n == uuids[uuid] && u < uuid {
			uuid = u
		}
	}
	return lib, uuid, err
}

// Present names what macOS already created in home's Application Support. Read it before
// anything restores or installs there, since the rule leaves these names out.
func Present(home string) map[string]bool {
	entries, _ := os.ReadDir(filepath.Join(home, "Library/Application Support")) // none yet: nothing to keep
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	return names
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
