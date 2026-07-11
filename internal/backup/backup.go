package backup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/digest"
	"github.com/Jszigeti/floe/internal/icloud"
	"github.com/Jszigeti/floe/internal/manifest"
	"github.com/Jszigeti/floe/internal/restic"
	"github.com/Jszigeti/floe/internal/run"
)

const lowQuotaBytes = 5 << 30

type Deps struct {
	Cfg    config.Config
	Client *restic.Client
	Runner run.Runner
	Notify func(title, msg string)
	Now    func() time.Time
	Log    func(format string, a ...any)
}

func Run(ctx context.Context, d Deps) error {
	if err := runHooks(ctx, d, d.Cfg.Hooks.Pre, "pre-hook"); err != nil {
		return err
	}

	if dir, err := manifest.Dir(); err == nil {
		if err := manifest.Dump(ctx, d.Runner, dir, "/Applications"); err != nil {
			d.Log("manifest dump skipped: %v", err)
		}
	}

	onICloud := icloud.IsICloudPath(d.Cfg.Destination)
	if onICloud {
		if rem, err := icloud.QuotaRemaining(ctx, d.Runner); err == nil && rem < lowQuotaBytes {
			d.Notify("floe: iCloud quota low",
				fmt.Sprintf("%.1f GiB remaining — reduce retention, add excludes, or upgrade iCloud+", float64(rem)/float64(1<<30)))
		}
	}

	d.Client.Unlock(ctx) // removes stale locks only; failure is non-fatal

	skipPrune := false
	if onICloud {
		// Fail closed: unknown sync state must never allow pruning an un-synced repo.
		if pending, err := icloud.SyncPending(ctx, d.Runner); err != nil || pending {
			skipPrune = true
			d.Log("repo sync pending or unknown, skipping prune this run")
		}
	}
	if !skipPrune {
		// A failed prune must never cost the daily snapshot — warn and continue.
		if err := d.Client.Forget(ctx, d.Cfg.Retention); err != nil {
			d.Notify("floe retention failed", err.Error())
		}
	}

	excludes := append([]string{d.Cfg.Destination}, d.Cfg.Scope.Exclude...)
	if err := d.Client.Backup(ctx, d.Cfg.Scope.Paths, excludes); err != nil {
		// Exit 3 still writes a snapshot — a daily "failed" alert for TCC-protected files would be crying wolf.
		if errors.Is(err, restic.ErrIncomplete) {
			d.Notify("floe: backup incomplete", "Some files could not be read — grant floe Full Disk Access in System Settings → Privacy & Security")
			d.Log("backup incomplete: %v", err)
		} else {
			d.Notify("floe backup failed", err.Error())
			return err
		}
	}

	if err := runHooks(ctx, d, d.Cfg.Hooks.Post, "post-hook"); err != nil {
		return err
	}

	if digest.Due(d.Now()) {
		msg := "Backups healthy"
		if size, err := d.Client.RepoSize(ctx); err == nil {
			msg = fmt.Sprintf("Backups healthy — repo %.1f GB", float64(size)/1e9)
		}
		d.Notify("floe: backups healthy", msg)
		if err := digest.MarkSent(d.Now()); err != nil {
			d.Log("digest stamp write failed: %v", err)
		}
	}
	return nil
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
