package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Jszigeti/floe/internal/backup"
	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/keychain"
	"github.com/Jszigeti/floe/internal/launchd"
	"github.com/Jszigeti/floe/internal/notify"
	"github.com/Jszigeti/floe/internal/restic"
	"github.com/Jszigeti/floe/internal/scan"
	"github.com/spf13/cobra"
)

func newInitCmd(d Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Set up floe: repository, passphrase, schedule, first backup",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			in := bufio.NewScanner(cmd.InOrStdin())
			ask := func(prompt, def string) string {
				cmd.Printf("%s [%s]: ", prompt, def)
				if in.Scan() {
					if v := strings.TrimSpace(in.Text()); v != "" {
						return v
					}
				}
				return def
			}

			cmd.Println("→ Ensuring restic binary…")
			bin, err := restic.EnsureBinary(ctx, "")
			if err != nil {
				return err
			}

			cfg, err := config.Default()
			if err != nil {
				return err
			}
			dest, err := normalizeDestination(ask("Backup destination", cfg.Destination))
			if err != nil {
				return err
			}
			cfg.Destination = dest

			if err := promptScope(cmd, ask, &cfg); err != nil {
				return err
			}

			pass, err := ensurePassphrase(ctx, cmd, d)
			if err != nil {
				return err
			}

			if h, err := strconv.Atoi(ask("Daily backup hour (0-23)", strconv.Itoa(cfg.Hour))); err == nil && h >= 0 && h <= 23 {
				cfg.Hour = h
			}

			client, cfgSaved, err := initRepoAndConfig(ctx, d, cfg, bin, pass, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			execPath, err := os.Executable()
			if err != nil {
				return err
			}
			if err := launchd.Install(ctx, d.Runner, execPath, cfgSaved.Hour); err != nil {
				return err
			}

			cmd.Println("→ Running first backup…")
			err = backup.Run(ctx, backup.Deps{
				Cfg: cfgSaved, Client: client, Runner: d.Runner,
				Notify: func(title, msg string) { notify.Send(ctx, d.Runner, title, msg) },
				Now:    time.Now,
				Log:    func(f string, a ...any) { cmd.Printf(f+"\n", a...) },
			})
			if err != nil {
				return err
			}
			cmd.Printf("✓ floe is set up. Daily backups at %s — check anytime with `floe status`.\n", formatHour(cfgSaved.Hour))
			return nil
		},
	}
}

// promptScope estimates the backup size and lets the user exclude directories from it.
func promptScope(cmd *cobra.Command, ask func(prompt, def string) string, cfg *config.Config) error {
	cmd.Println("→ Estimating backup size (this can take a minute)…")
	home, _ := os.UserHomeDir()
	total, top, err := scan.Estimate(home, cfg.Scope.Exclude)
	if err != nil {
		return err
	}
	cmd.Printf("Estimated backup size: %.1f GB\nLargest directories:\n", float64(total)/1e9)
	n := min(10, len(top))
	for i := 0; i < n; i++ {
		cmd.Printf("  %2d. %6.1f GB  %s\n", i+1, float64(top[i].Bytes)/1e9, top[i].Path)
	}
	if picks := ask("Exclude numbers (comma-separated)", ""); picks != "" {
		for _, s := range strings.Split(picks, ",") {
			if i, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && i >= 1 && i <= n {
				cfg.Scope.Exclude = append(cfg.Scope.Exclude, top[i-1].Path)
			}
		}
	}
	return nil
}

// ensurePassphrase reuses an existing Keychain passphrase, or generates and stores a new
// one — shown once since it is never displayed again after this run.
func ensurePassphrase(ctx context.Context, cmd *cobra.Command, d Deps) (string, error) {
	pass, err := keychain.Get(ctx, d.Runner)
	// Only regenerate on a confirmed missing entry — overwriting on a flaky read would orphan an existing repo.
	if err != nil && !errors.Is(err, keychain.ErrNotFound) {
		return "", err
	}
	if err != nil {
		pass, err = keychain.Generate()
		if err != nil {
			return "", err
		}
		if err := keychain.Set(ctx, d.Runner, pass); err != nil {
			return "", err
		}
		cmd.Println("\n════════════════════════════════════════════════")
		cmd.Printf("  Your backup passphrase: %s\n", pass)
		cmd.Println("  Save it in your password manager NOW.")
		cmd.Println("  Without it your backup is unreadable.")
		cmd.Println("════════════════════════════════════════════════")
	} else {
		cmd.Println("→ Reusing existing passphrase from Keychain")
	}
	return pass, nil
}

// normalizeDestination expands a leading ~ or $HOME, requires an absolute
// path, and requires the directory to already exist — the destination is an
// already-synced folder, so creating it would never be correct.
func normalizeDestination(input string) (string, error) {
	input, err := config.NormalizeDestination(input)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(input); err != nil || !info.IsDir() {
		return "", fmt.Errorf("destination %s is not an existing directory — pick an existing synced folder", input)
	}
	return input, nil
}

func initRepoAndConfig(ctx context.Context, d Deps, cfg config.Config, bin, pass string, progress io.Writer) (*restic.Client, config.Config, error) {
	repo, err := cfg.RepoPath()
	if err != nil {
		return nil, cfg, err
	}
	if err := os.MkdirAll(repo, 0o755); err != nil {
		return nil, cfg, err
	}
	client := &restic.Client{Bin: bin, Repo: repo, Pass: pass, R: d.Runner, Progress: progress}
	initialized, err := client.IsInitialized(ctx)
	if err != nil {
		return nil, cfg, fmt.Errorf("checking repository: %w", err)
	}
	if !initialized {
		if err := client.Init(ctx); err != nil {
			return nil, cfg, fmt.Errorf("initializing repository: %w", err)
		}
	}
	if err := cfg.Save(); err != nil {
		return nil, cfg, err
	}
	return client, cfg, nil
}
