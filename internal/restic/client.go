package restic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"

	"github.com/Jszigeti/floe/internal/run"
)

// ErrIncomplete indicates restic exited 3: a snapshot was written but some
// source files could not be read (typically TCC-protected dirs like ~/Library/Mail).
var ErrIncomplete = errors.New("backup incomplete: some files could not be read")

// maxUnreadablePaths bounds how many paths IncompleteError keeps, so a
// pathological run (thousands of TCC-denied files) never balloons memory.
const maxUnreadablePaths = 1000

// unreadablePathRe matches restic's two exit-3 stderr shapes:
// "error: open <path>: ..." and "error: openfile for readdirnames failed: open <path>: ...".
// No line-start anchor: ExecRunner folds stderr into a single error string, so
// the first occurrence isn't preceded by a newline.
var unreadablePathRe = regexp.MustCompile(`error: (?:openfile for readdirnames failed: )?open (.+?):`)

// IncompleteError carries the individual files restic could not read on an
// exit-3 (incomplete) backup, so callers can summarize instead of dumping
// restic's raw stderr (which embeds the full argv and can run to hundreds of lines).
type IncompleteError struct {
	Unreadable []string
	Err        error
}

func (e *IncompleteError) Error() string {
	return fmt.Sprintf("backup incomplete: %d files could not be read", len(e.Unreadable))
}

// Unwrap returns the ErrIncomplete sentinel (not Err) so callers keep using
// errors.Is(err, ErrIncomplete) without depending on this type.
func (e *IncompleteError) Unwrap() error {
	return ErrIncomplete
}

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

func (c *Client) IsInitialized(ctx context.Context) (bool, error) {
	_, err := c.run(ctx, "cat", "config")
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	// restic ≥0.17 documents exit 10 for "repository does not exist" — stable, unlike the stderr prose.
	if errors.As(err, &ee) && ee.ExitCode() == 10 {
		return false, nil
	}
	return false, err
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
		return &IncompleteError{Unreadable: parseUnreadablePaths(err.Error()), Err: err}
	}
	return err
}

func parseUnreadablePaths(msg string) []string {
	matches := unreadablePathRe.FindAllStringSubmatch(msg, maxUnreadablePaths)
	paths := make([]string, len(matches))
	for i, m := range matches {
		paths[i] = m[1]
	}
	return paths
}
