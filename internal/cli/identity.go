package cli

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/notify"
	"github.com/Jszigeti/floe/internal/run"
)

// foreignConfigMsg is what a config copied from another Mac says instead of backing up.
const foreignConfigMsg = "This floe config comes from another Mac — run floe init"

var errForeignConfig = errors.New("this floe config comes from another Mac — run floe init")

var platformUUIDRe = regexp.MustCompile(`"IOPlatformUUID" = "([0-9A-Fa-f-]+)"`)

// machineID reads the hardware UUID: Migration Assistant copies files, never this.
func machineID(ctx context.Context, r run.Runner) (string, error) {
	out, err := r.Run(ctx, run.Cmd{Name: "ioreg", Args: []string{"-rd1", "-c", "IOPlatformExpertDevice"}})
	if err != nil {
		return "", err
	}
	m := platformUUIDRe.FindStringSubmatch(out)
	if m == nil {
		return "", errors.New("ioreg printed no IOPlatformUUID")
	}
	return m[1], nil
}

// adoptIdentity records the repo and the Mac that a config from before v0.2.0 lacks.
func adoptIdentity(ctx context.Context, r run.Runner, cfg *config.Config) error {
	if cfg.Repo != "" && cfg.Machine != "" {
		return nil
	}
	repo, err := cfg.RepoPath()
	if err != nil {
		return err
	}
	cfg.Repo = repo
	if cfg.Machine == "" {
		// An unreadable id stays empty and is retried on the next run.
		cfg.Machine, _ = machineID(ctx, r)
	}
	return cfg.Save()
}

// foreignConfig reports a config written on another Mac; one with no recorded Mac is never foreign.
func foreignConfig(ctx context.Context, r run.Runner, cfg config.Config) (bool, error) {
	if cfg.Machine == "" {
		return false, nil
	}
	mac, err := machineID(ctx, r)
	if err != nil {
		return false, fmt.Errorf("reading this Mac's hardware UUID: %w", err)
	}
	return mac != cfg.Machine, nil
}

// stopForeignConfig fails, and notifies, when the config was written on another Mac.
func stopForeignConfig(ctx context.Context, r run.Runner, cfg config.Config) error {
	foreign, err := foreignConfig(ctx, r, cfg)
	if err != nil || !foreign {
		return err
	}
	// The returned error already reaches floe.log; a failed notification adds nothing to it.
	_ = notify.Send(ctx, r, "floe backup stopped", foreignConfigMsg)
	return errForeignConfig
}
