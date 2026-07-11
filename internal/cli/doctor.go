package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/icloud"
	"github.com/Jszigeti/floe/internal/keychain"
	"github.com/Jszigeti/floe/internal/launchd"
	"github.com/Jszigeti/floe/internal/restic"
	"github.com/spf13/cobra"
)

// interpretFDAProbe classifies the result of reading a TCC-protected directory
// into a doctor check's (ok, warn, msg) tuple.
func interpretFDAProbe(err error) (bool, bool, string) {
	if errors.Is(err, fs.ErrPermission) {
		return true, true, "denied — Mail, Messages and Safari data will be skipped; grant Full Disk Access to your terminal and to floe in System Settings → Privacy & Security"
	}
	return true, false, "ok"
}

type check struct {
	name string
	run  func(ctx context.Context) (ok bool, warn bool, msg string)
}

func newDoctorCmd(d Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose backup health and iCloud pitfalls",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			repo, err := cfg.RepoPath()
			if err != nil {
				return err
			}
			checks := []check{
				{"restic binary", func(ctx context.Context) (bool, bool, string) {
					bin, err := restic.BinPath()
					if err != nil {
						return false, false, err.Error()
					}
					if _, err := os.Stat(bin); err != nil {
						return false, false, "missing — run `floe init`"
					}
					return true, false, bin
				}},
				{"keychain passphrase", func(ctx context.Context) (bool, bool, string) {
					if _, err := keychain.Get(ctx, d.Runner); err != nil {
						return false, false, "missing — run `floe init`"
					}
					return true, false, "present"
				}},
				{"schedule", func(ctx context.Context) (bool, bool, string) {
					if !launchd.IsLoaded(ctx, d.Runner) {
						return false, false, "launchagent not loaded — run `floe init`"
					}
					return true, false, "daily at " + formatHour(cfg.Hour)
				}},
				{"repository", func(ctx context.Context) (bool, bool, string) {
					client, _, err := buildClient(ctx, d.Runner)
					if err != nil {
						return false, false, err.Error()
					}
					if !client.IsInitialized(ctx) {
						return false, false, "unreachable or not initialized"
					}
					return true, false, repo
				}},
				{"full disk access", func(ctx context.Context) (bool, bool, string) {
					home, err := os.UserHomeDir()
					if err != nil {
						return false, false, err.Error()
					}
					// ~/Library/Mail is TCC-protected: readable only with Full Disk Access.
					_, err = os.ReadDir(filepath.Join(home, "Library/Mail"))
					return interpretFDAProbe(err)
				}},
			}
			if icloud.IsICloudPath(cfg.Destination) {
				checks = append(checks,
					check{"optimize mac storage", func(ctx context.Context) (bool, bool, string) {
						if icloud.OptimizeStorageEnabled(ctx, d.Runner) {
							return true, true, "ON — macOS may evict repo files; disable it in System Settings → Apple ID → iCloud"
						}
						return true, false, "off"
					}},
					check{"repo files materialized", func(ctx context.Context) (bool, bool, string) {
						dataless, err := icloud.DatalessFiles(repo, icloud.StatFlags)
						if err != nil {
							return true, true, fmt.Sprintf("could not scan for evicted files: %v", err)
						}
						if len(dataless) == 0 {
							return true, false, "all local"
						}
						if err := icloud.Materialize(ctx, d.Runner, icloud.StatFlags, dataless); err != nil {
							return false, false, fmt.Sprintf("%d evicted files, re-download failed: %v", len(dataless), err)
						}
						return true, true, fmt.Sprintf("re-downloaded %d evicted files", len(dataless))
					}},
					check{"icloud quota", func(ctx context.Context) (bool, bool, string) {
						rem, err := icloud.QuotaRemaining(ctx, d.Runner)
						if err != nil {
							return true, true, "unknown"
						}
						if rem < 5<<30 {
							return true, true, fmt.Sprintf("only %.1f GiB remaining", float64(rem)/float64(1<<30))
						}
						return true, false, fmt.Sprintf("%.1f GiB remaining", float64(rem)/float64(1<<30))
					}},
				)
			}
			failed := false
			for _, c := range checks {
				ok, warn, msg := c.run(ctx)
				marker := "✓"
				if warn {
					marker = "!"
				}
				if !ok {
					marker, failed = "✗", true
				}
				cmd.Printf("%s %-26s %s\n", marker, c.name, msg)
			}
			if failed {
				return fmt.Errorf("doctor found problems")
			}
			return nil
		},
	}
}
