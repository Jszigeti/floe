package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jszigeti/floe/internal/backup"
	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/icloud"
	"github.com/Jszigeti/floe/internal/keychain"
	"github.com/Jszigeti/floe/internal/launchd"
	"github.com/Jszigeti/floe/internal/restic"
	"github.com/spf13/cobra"
)

// hostAppHint names the app that needs Full Disk Access granted alongside the
// floe binary — the terminal or editor running the shell floe was launched
// from, identified via TERM_PROGRAM (set by Terminal.app, iTerm2, and
// VS Code/Cursor's integrated terminal).
func hostAppHint() string {
	switch os.Getenv("TERM_PROGRAM") {
	case "Apple_Terminal":
		return "Terminal"
	case "iTerm.app":
		return "iTerm"
	case "vscode":
		return "your editor's app (VS Code or Cursor)"
	default:
		return "your terminal app"
	}
}

// interpretFDAProbe classifies the result of reading a TCC-protected directory
// into a doctor check's (ok, warn, msg) tuple.
func interpretFDAProbe(err error) (bool, bool, string) {
	if errors.Is(err, fs.ErrPermission) {
		return true, true, fmt.Sprintf("denied — Mail, Messages and Safari data will be skipped; grant Full Disk Access to %s and to the floe binary in System Settings → Privacy & Security", hostAppHint())
	}
	return true, false, "ok"
}

// lastIncompleteReport reads last_incomplete.log, the report a scheduled
// backup writes when Full Disk Access blocks it from reading protected
// files, and returns how many paths it lists and when it was written.
func lastIncompleteReport() (count int, when time.Time, err error) {
	p, err := backup.IncompleteLogPath()
	if err != nil {
		return 0, time.Time{}, err
	}
	info, err := os.Stat(p)
	if err != nil {
		return 0, time.Time{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return 0, time.Time{}, err
	}
	// A 0-byte or whitespace-only file (a run killed mid-write, since
	// writeIncompleteLog truncates before writing) must count as zero, not
	// one blank entry.
	count = 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count, info.ModTime(), nil
}

// floe.log only receives scheduled runs: the LaunchAgent redirects into it, a manual run prints to the terminal.
func lastScheduledFailure() (when time.Time, failed bool) {
	p, err := launchd.LogPath()
	if err != nil {
		return time.Time{}, false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return time.Time{}, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		stamp, event, _ := strings.Cut(line, " ")
		at, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			continue
		}
		if event == "backup finished" {
			failed = false
		}
		if strings.HasPrefix(event, "backup failed") {
			when, failed = at, true
		}
	}
	return when, failed
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
					// An upgraded plist predating FLOE_SCHEDULED would silently stop
					// reporting incomplete scheduled runs — surface that instead of
					// claiming "complete" forever.
					if pp, err := launchd.PlistPath(); err == nil {
						if content, err := os.ReadFile(pp); err == nil && !strings.Contains(string(content), "FLOE_SCHEDULED") {
							return true, true, fmt.Sprintf(
								"daily at %s — schedule predates this version, re-run `floe init` so incomplete runs get reported",
								formatHour(cfg.Hour))
						}
					}
					return true, false, "daily at " + formatHour(cfg.Hour)
				}},
				{"repository", func(ctx context.Context) (bool, bool, string) {
					client, _, err := buildClient(ctx, d.Runner)
					if err != nil {
						return false, false, err.Error()
					}
					initialized, err := client.IsInitialized(ctx)
					if err != nil {
						return false, false, err.Error()
					}
					if !initialized {
						return false, false, "unreachable or not initialized"
					}
					return true, false, repo
				}},
				{"full disk access (this shell)", func(ctx context.Context) (bool, bool, string) {
					home, err := os.UserHomeDir()
					if err != nil {
						return false, false, err.Error()
					}
					// ~/Library/Mail is TCC-protected: readable only with Full Disk Access.
					_, err = os.ReadDir(filepath.Join(home, "Library/Mail"))
					return interpretFDAProbe(err)
				}},
				{"last scheduled backup", func(ctx context.Context) (bool, bool, string) {
					if when, failed := lastScheduledFailure(); failed {
						logPath, _ := launchd.LogPath() // lastScheduledFailure already resolved it
						return false, false, fmt.Sprintf("failed on %s — see %s", when.Format("2006-01-02 3:04 PM"), logPath)
					}
					count, when, err := lastIncompleteReport()
					if err != nil {
						if errors.Is(err, fs.ErrNotExist) {
							return true, false, "complete"
						}
						return true, false, "unknown"
					}
					bin, err := os.Executable()
					if err != nil {
						bin = "the floe binary"
					}
					return true, true, fmt.Sprintf(
						"skipped %d protected files on %s — macOS ties Full Disk Access to the exact binary, so re-grant it to %s after every upgrade (System Settings → Privacy & Security)",
						count, when.Format("2006-01-02 3:04 PM"), bin)
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
