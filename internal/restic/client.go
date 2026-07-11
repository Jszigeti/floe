package restic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"

	"github.com/Jszigeti/floe/internal/run"
)

// ErrIncomplete indicates restic exited 3: a snapshot was written but some
// source files could not be read (typically TCC-protected dirs like ~/Library/Mail).
var ErrIncomplete = errors.New("backup incomplete: some files could not be read")

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
		return classifyBackupErr(err)
	}
	_, err := c.R.Run(ctx, run.Cmd{Name: c.Bin, Args: args, Env: c.env("RESTIC_PROGRESS_FPS=1"), Stdout: c.Progress})
	return classifyBackupErr(err)
}

func classifyBackupErr(err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 3 {
		return fmt.Errorf("%w: %v", ErrIncomplete, err)
	}
	return err
}
