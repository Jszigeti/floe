package backup

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/digest"
	"github.com/Jszigeti/floe/internal/icloud"
	"github.com/Jszigeti/floe/internal/inventory"
	"github.com/Jszigeti/floe/internal/manifest"
	"github.com/Jszigeti/floe/internal/restic"
	"github.com/Jszigeti/floe/internal/run"
)

const lowQuotaBytes = 5 << 30

const maxIncompleteExamples = 3

// IncompleteLogPath returns $HOME/.local/state/floe/last_incomplete.log,
// where logIncomplete writes the full unreadable-path list every run.
func IncompleteLogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local/state/floe/last_incomplete.log"), nil
}

func writeIncompleteLog(paths []string) error {
	p, err := IncompleteLogPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(strings.Join(paths, "\n")), 0o644)
}

// clearIncompleteLog removes the previous run's incomplete-backup report.
// A complete run invalidates the previous failure report.
func clearIncompleteLog(d Deps) {
	p, err := IncompleteLogPath()
	if err != nil {
		d.Log("incomplete-log removal failed: %v", err)
		return
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		d.Log("incomplete-log removal failed: %v", err)
	}
}

// logIncomplete summarizes a restic.IncompleteError into one short log line
// instead of the raw error (which embeds the full restic argv and can carry
// hundreds of TCC-denial lines from restic's stderr), and writes the full
// unreadable-path list to state for later inspection.
func logIncomplete(d Deps, err error) {
	var ie *restic.IncompleteError
	errors.As(err, &ie) // guaranteed by caller (errors.Is check), but extract for clarity

	// Fall back to raw detail when the unreadable paths list is empty (regex did not match).
	if len(ie.Unreadable) == 0 {
		d.Log("backup incomplete: some files could not be read (grant Full Disk Access — see floe doctor); detail: %s", ie.RawDetail)
		return
	}

	// Lives inside the backup scope on purpose: the last failure report travels with the snapshots.
	if d.Scheduled {
		if werr := writeIncompleteLog(ie.Unreadable); werr != nil {
			d.Log("incomplete-log write failed: %v", werr)
		}
	}
	examples := ie.Unreadable
	if len(examples) > maxIncompleteExamples {
		examples = examples[:maxIncompleteExamples]
	}
	msg := fmt.Sprintf("backup incomplete: %d files could not be read (grant Full Disk Access — see floe doctor)", len(ie.Unreadable))
	msg += fmt.Sprintf("; examples: %s", strings.Join(examples, ", "))
	d.Log("%s; full list: ~/.local/state/floe/last_incomplete.log", msg)
}

type Deps struct {
	Cfg    config.Config
	Client *restic.Client
	Runner run.Runner
	Notify func(title, msg string)
	Now    func() time.Time
	Log    func(format string, a ...any)
	// Scheduled is true only for the launchd-triggered run (see FLOE_SCHEDULED
	// in internal/launchd). A manual `floe backup` never owns the incomplete
	// report — see logIncomplete and the Run backup branch.
	Scheduled bool
	AppRoots  []string // folders whose apps are inventoried, none in tests
	CaskURL   string   // Homebrew's cask list, empty in tests
}

func statePath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local/state/floe", name), nil
}

func Run(ctx context.Context, d Deps) error {
	if err := runHooks(ctx, d, d.Cfg.Hooks.Pre, "pre-hook"); err != nil {
		return err
	}

	caskCache, _ := statePath("cask.json") // no home: no manifest, nothing to leave out
	apps := dumpManifest(ctx, d, caskCache)

	onICloud := icloud.IsICloudPath(d.Cfg.Destination)
	if onICloud {
		if rem, err := icloud.QuotaRemaining(ctx, d.Runner); err == nil && rem < lowQuotaBytes {
			d.Notify("floe: iCloud quota low",
				fmt.Sprintf("%.1f GiB remaining — reduce retention, add excludes, or upgrade iCloud+", float64(rem)/float64(1<<30)))
		}
	}

	_ = d.Client.Unlock(ctx) // removes stale locks only; failure is non-fatal
	migrateTags(ctx, d)

	skipPrune := false
	if onICloud {
		// Fail closed: unknown sync state must never allow pruning an un-synced repo.
		if pending, err := icloud.SyncPending(ctx, d.Runner); err != nil || pending {
			skipPrune = true
			d.Log("repo sync pending or unknown, skipping prune this run")
		}
	}
	if !skipPrune {
		dropReplacedTag(ctx, d)
		// A failed prune must never cost the daily snapshot — warn and continue.
		if err := d.Client.Forget(ctx, d.Cfg.Retention); err != nil {
			d.Notify("floe retention failed", err.Error())
		}
	}

	// Before the home snapshot, so the newest snapshot stays the home one for `restore latest` and `status`.
	backupApps(ctx, d, apps)

	excludes := append([]string{d.Cfg.Destination}, d.Cfg.Scope.Exclude...)
	if caskCache != "" {
		excludes = append(excludes, caskCache) // refreshed weekly and never restored
	}
	incomplete := false
	if err := d.Client.Backup(ctx, d.Cfg.Scope.Paths, excludes, []string{"home"}); err != nil {
		// Exit 3 still writes a snapshot — a daily "failed" alert for TCC-protected files would
		// be crying wolf, so the notice is weekly.
		if errors.Is(err, restic.ErrIncomplete) {
			incomplete = true
			logIncomplete(d, err)
			if digest.NoticeDue("last_incomplete_notice", d.Now()) {
				d.Notify("floe: backup incomplete", "Some files could not be read — grant floe Full Disk Access in System Settings → Privacy & Security")
				if err := digest.MarkNotice("last_incomplete_notice", d.Now()); err != nil {
					d.Log("incomplete-notice stamp write failed: %v", err)
				}
			}
		} else {
			d.Notify("floe backup failed, run floe doctor", err.Error())
			return err
		}
	} else if d.Scheduled {
		// Only the scheduled run owns this report: a manual run has the terminal's
		// permissions and would clear evidence of a broken schedule.
		clearIncompleteLog(d)
	}

	if err := runHooks(ctx, d, d.Cfg.Hooks.Post, "post-hook"); err != nil {
		return err
	}

	if digest.Due(d.Now()) {
		title, msg := "floe: backups healthy", "Backups healthy"
		if incomplete {
			title = "floe: backups running"
			msg = "Backups running, some files skipped every run — grant Full Disk Access (see floe doctor)"
		} else if size, err := d.Client.RepoSize(ctx); err == nil {
			msg = fmt.Sprintf("Backups healthy — repo %.1f GB", float64(size)/1e9)
		}
		if recipe := incompleteRecipe(); recipe != "" {
			msg += "; " + recipe
		}
		d.Notify(title, msg)
		if err := digest.MarkSent(d.Now()); err != nil {
			d.Log("digest stamp write failed: %v", err)
		}
	}
	return nil
}

// dumpManifest writes the recipe and returns the app inventory; nothing in it ever fails the backup.
func dumpManifest(ctx context.Context, d Deps, caskCache string) []inventory.App {
	dir, err := manifest.Dir()
	if err != nil {
		return nil
	}
	casks, err := inventory.LoadCasks(ctx, caskCache, d.CaskURL, 7*24*time.Hour, d.Now())
	if err != nil {
		d.Log("manifest: %v", err)
	}
	apps, warnings, err := manifest.Dump(ctx, d.Runner, dir, manifest.Options{AppRoots: d.AppRoots, Casks: casks, Now: d.Now()})
	if err != nil {
		d.Log("manifest dump skipped: %v", err)
	}
	for _, w := range warnings {
		d.Log("manifest: %s", w)
	}
	return apps
}

// backupApps saves the bundles no installer can bring back in their own snapshot, without the
// home's excludes: an unanchored "Cache" or "node_modules" would break a bundle's signature.
func backupApps(ctx context.Context, d Deps, apps []inventory.App) {
	var paths []string
	for _, a := range apps {
		if a.Source == inventory.SourceBackup {
			paths = append(paths, a.Path)
		}
	}
	if len(paths) == 0 {
		return
	}
	err := d.Client.Backup(ctx, paths, nil, []string{"apps"})
	var ie *restic.IncompleteError
	switch {
	case errors.As(err, &ie):
		// No alert: an app updated during the run is common; the files left out are named in the log.
		if len(ie.Unreadable) == 0 {
			// A removed app gives "<path> does not exist, skipping", which names no unread file.
			d.Log("apps backup incomplete, the rest is saved; detail: %s", ie.RawDetail)
			break
		}
		examples := ie.Unreadable
		if len(examples) > maxIncompleteExamples {
			examples = examples[:maxIncompleteExamples]
		}
		d.Log("apps backup incomplete: %d files could not be read, the rest is saved; examples: %s", len(ie.Unreadable), strings.Join(examples, ", "))
	case err != nil:
		d.Log("apps backup failed: %v", err)
		d.Notify("floe: app backup failed", "Apps that no installer can bring back were not saved: run floe backup in Terminal to see why")
	}
}

// replacedKept is how long forget keeps the snapshot a new Mac was restored from, so the old Mac's
// Mail, Messages and keychain stay reachable after it is gone.
const replacedKept = 365 * 24 * time.Hour

// dropReplacedTag hands the snapshots replaced over a year ago back to the retention policy.
func dropReplacedTag(ctx context.Context, d Deps) {
	snaps, err := d.Client.Snapshots(ctx)
	if err != nil {
		d.Log("reading snapshots for the replaced tag failed: %v", err)
		return
	}
	for _, s := range snaps {
		if slices.Contains(s.Tags, "replaced") && d.Now().Sub(s.Time) > replacedKept {
			if _, err := d.Client.Tag(ctx, s.ShortID, nil, []string{"replaced"}); err != nil {
				d.Log("untagging replaced snapshot %s failed: %v", s.ShortID, err)
			}
		}
	}
}

// migrateTags tags the snapshots made before snapshots carried their kind, once.
func migrateTags(ctx context.Context, d Deps) {
	// One stamp per repository: a Mac that switches to another backup must migrate that one too.
	name := "tags-migrated-" + fmt.Sprintf("%x", sha256.Sum256([]byte(d.Client.Repo)))[:12]
	stamp, err := statePath(name)
	if err != nil {
		return
	}
	if _, err := os.Stat(stamp); err == nil {
		return
	}
	if err := d.Client.TagUntagged(ctx, "home"); err != nil {
		d.Log("tagging older snapshots failed, retrying next run: %v", err)
		return
	}
	if err := digest.MarkNotice(name, d.Now()); err != nil {
		d.Log("tags-migrated stamp write failed: %v", err)
	}
}

func incompleteRecipe() string {
	dir, err := manifest.Dir()
	if err != nil {
		return ""
	}
	failed, err := manifest.Failures(dir)
	if err != nil {
		return ""
	}
	return manifest.Incomplete(failed)
}

func runHooks(ctx context.Context, d Deps, hooks []string, kind string) error {
	for _, h := range hooks {
		if _, err := d.Runner.Run(ctx, run.Cmd{Name: "sh", Args: []string{"-c", h}}); err != nil {
			d.Notify("floe backup failed", fmt.Sprintf("%s failed: %s", kind, h))
			return fmt.Errorf("%s %q: %w", kind, h, err)
		}
	}
	return nil
}
