package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/keychain"
	"github.com/Jszigeti/floe/internal/restic"
	"github.com/Jszigeti/floe/internal/run"
)

func buildClient(ctx context.Context, r run.Runner) (*restic.Client, config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, config.Config{}, err
	}
	pass, err := keychain.Get(ctx, r)
	if err != nil {
		return nil, config.Config{}, err
	}
	bin, err := restic.BinPath()
	if err != nil {
		return nil, config.Config{}, err
	}
	if _, err := os.Stat(bin); err != nil {
		return nil, config.Config{}, fmt.Errorf("restic binary missing at %s — run `floe init`", bin)
	}
	repo, err := cfg.RepoPath()
	if err != nil {
		return nil, config.Config{}, err
	}
	return &restic.Client{Bin: bin, Repo: repo, Pass: pass, R: r}, cfg, nil
}
