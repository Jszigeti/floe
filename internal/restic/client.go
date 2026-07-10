package restic

import (
	"context"
	"io"

	"github.com/Jszigeti/floe/internal/run"
)

type Client struct {
	Bin      string
	Repo     string
	Pass     string
	R        run.Runner
	Progress io.Writer // when set, Backup streams restic's progress output here instead of running --quiet
}

func (c *Client) env(extra ...string) []string {
	return append([]string{
		"RESTIC_REPOSITORY=" + c.Repo,
		"RESTIC_PASSWORD=" + c.Pass,
	}, extra...)
}

func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	return c.R.Run(ctx, run.Cmd{Name: c.Bin, Args: args, Env: c.env()})
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
	args := []string{"backup"}
	if c.Progress == nil {
		args = append(args, "--quiet")
	}
	args = append(args, paths...)
	for _, e := range excludes {
		args = append(args, "--exclude", e)
	}
	if c.Progress == nil {
		_, err := c.run(ctx, args...)
		return err
	}
	_, err := c.R.Run(ctx, run.Cmd{Name: c.Bin, Args: args, Env: c.env("RESTIC_PROGRESS_FPS=1"), Stdout: c.Progress})
	return err
}
