package restic

import (
	"context"

	"github.com/Jszigeti/floe/internal/run"
)

type Client struct {
	Bin  string
	Repo string
	Pass string
	R    run.Runner
}

func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	return c.R.Run(ctx, run.Cmd{Name: c.Bin, Args: args, Env: []string{
		"RESTIC_REPOSITORY=" + c.Repo,
		"RESTIC_PASSWORD=" + c.Pass,
	}})
}

func (c *Client) IsInitialized(ctx context.Context) bool {
	_, err := c.run(ctx, "cat", "config")
	return err == nil
}

func (c *Client) Init(ctx context.Context) error {
	_, err := c.run(ctx, "init")
	return err
}

func (c *Client) Backup(ctx context.Context, paths, excludes []string) error {
	args := append([]string{"backup", "--quiet"}, paths...)
	for _, e := range excludes {
		args = append(args, "--exclude", e)
	}
	_, err := c.run(ctx, args...)
	return err
}
