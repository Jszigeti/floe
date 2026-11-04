package restic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"

	"github.com/Jszigeti/floe/internal/run"
)

// ErrWrongPassphrase means restic exited 12: the passphrase does not open the repository.
var ErrWrongPassphrase = errors.New("this passphrase does not open the backup")

// restoreErrorRe matches restic's "ignoring error for <path>: <cause>", a file the restore skipped.
var restoreErrorRe = regexp.MustCompile(`ignoring error for (.+?): `)

// PartialRestoreError lists the paths, relative to the restored folder, restic could not write.
type PartialRestoreError struct {
	Failed []string
}

func (e *PartialRestoreError) Error() string {
	return fmt.Sprintf("%d paths could not be restored", len(e.Failed))
}

// Open checks that the passphrase opens the repository.
func (c *Client) Open(ctx context.Context) error {
	_, err := c.run(ctx, "cat", "config")
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 12 {
		return ErrWrongPassphrase
	}
	return err
}

// Latest is the newest snapshot: `snapshots --latest 1` keeps one per host and path, not one overall.
func (c *Client) Latest(ctx context.Context) (Snapshot, error) {
	snaps, err := c.Snapshots(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if len(snaps) == 0 {
		return Snapshot{}, errors.New("the backup has no snapshot yet")
	}
	return slices.MaxFunc(snaps, func(a, b Snapshot) int { return a.Time.Compare(b.Time) }), nil
}

// RestoreSize is the size of the files snapshot id restores.
func (c *Client) RestoreSize(ctx context.Context, id string) (int64, error) {
	out, err := c.run(ctx, "stats", id, "--mode", "restore-size", "--json")
	if err != nil {
		return 0, err
	}
	return parseTotalSize(out)
}

func parseTotalSize(out string) (int64, error) {
	var stats struct {
		TotalSize int64 `json:"total_size"`
	}
	if err := json.Unmarshal([]byte(out), &stats); err != nil {
		return 0, fmt.Errorf("parsing stats: %w", err)
	}
	return stats.TotalSize, nil
}

// RestoreFolder restores folder of snapshot id into target, streaming progress to c.Progress.
// Excludes starting with / are anchored at folder. Skipped files return a *PartialRestoreError.
func (c *Client) RestoreFolder(ctx context.Context, id, folder, target string, excludes []string) error {
	args := []string{"restore", id + ":" + folder, "--target", target}
	for _, e := range excludes {
		args = append(args, "--exclude", e)
	}
	_, err := c.R.Run(ctx, run.Cmd{Name: c.Bin, Args: args, Env: c.env("RESTIC_PROGRESS_FPS=1"), Stdout: c.Progress})
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		return err
	}
	matches := restoreErrorRe.FindAllStringSubmatch(err.Error(), maxUnreadablePaths)
	if len(matches) == 0 {
		return err
	}
	paths := make([]string, len(matches))
	for i, m := range matches {
		paths[i] = m[1]
	}
	slices.Sort(paths)
	return &PartialRestoreError{Failed: slices.Compact(paths)}
}

// Dump returns one file of snapshot id.
func (c *Client) Dump(ctx context.Context, id, path string) (string, error) {
	return c.run(ctx, "dump", id, path)
}
