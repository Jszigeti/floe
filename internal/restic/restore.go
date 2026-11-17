package restic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"github.com/Jszigeti/floe/internal/run"
)

// ErrWrongPassphrase means restic exited 12: the passphrase does not open the repository.
var ErrWrongPassphrase = errors.New("this passphrase does not open the backup")

// ErrNoSnapshot means no snapshot of the asked kind exists.
var ErrNoSnapshot = errors.New("the backup has no snapshot yet")

// ErrNotInSnapshot means Dump asked for a path the snapshot does not hold.
var ErrNotInSnapshot = errors.New("not in the snapshot")

// notInSnapshot is the line restic 0.19.1 writes on stderr when a dumped path is missing.
var notInSnapshot = regexp.MustCompile(`Fatal: cannot dump file: path ".*" not found in snapshot`)

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

// Is reports whether the snapshot is of kind tag; snapshots made before kinds existed are home.
func (s Snapshot) Is(tag string) bool {
	return slices.Contains(s.Tags, tag) || (tag == TagHome && len(s.Tags) == 0)
}

// Latest is the newest snapshot carrying tag: `snapshots --latest 1` keeps one per host and path,
// not one overall. Snapshots with no tag were made before tags existed and count as home.
func (c *Client) Latest(ctx context.Context, tag string) (Snapshot, error) {
	snaps, err := c.Snapshots(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	snaps = slices.DeleteFunc(snaps, func(s Snapshot) bool { return !s.Is(tag) })
	if len(snaps) == 0 {
		return Snapshot{}, ErrNoSnapshot
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

// RestoreStatus is how far a restore is, in bytes: Done counts restored and skipped bytes, Total
// the bytes to restore plus the skipped ones, Skipped the bytes already in place, which a resume
// skips at once. Loading means restic has not counted any file yet.
type RestoreStatus struct {
	Done, Total, Skipped int64
	Loading              bool
}

// restoreLine is one line of `restore --json`: a status or summary on stdout, an error on stderr.
type restoreLine struct {
	MessageType   string `json:"message_type"`
	TotalBytes    int64  `json:"total_bytes"`
	BytesRestored int64  `json:"bytes_restored"`
	BytesSkipped  int64  `json:"bytes_skipped"`
	Item          string `json:"item"`
}

// RestoreFolder restores folder of snapshot id into target, skipping the patterns of excludeFile,
// anchored at folder. status gets every progress line, from the goroutine reading restic's output.
// Files restic could not write return a *PartialRestoreError.
func (c *Client) RestoreFolder(ctx context.Context, id, folder, target, excludeFile string, status func(RestoreStatus)) error {
	w := &lineWriter{each: func(line []byte) {
		var l restoreLine
		if json.Unmarshal(line, &l) != nil || (l.MessageType != "status" && l.MessageType != "summary") {
			return
		}
		total := l.TotalBytes + l.BytesSkipped
		status(RestoreStatus{Done: l.BytesRestored + l.BytesSkipped, Total: total, Skipped: l.BytesSkipped, Loading: total == 0})
	}}
	_, err := c.R.Run(ctx, run.Cmd{
		Name:   c.Bin,
		Args:   []string{"restore", "--json", id + ":" + folder, "--target", target, "--exclude-file", excludeFile},
		Env:    c.env("RESTIC_PROGRESS_FPS=1"),
		Stdout: w,
	})
	w.Flush()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		return err
	}
	if failed := restoreErrors(err.Error()); len(failed) > 0 {
		return &PartialRestoreError{Failed: failed}
	}
	return err
}

// restoreErrors are the items of the error lines restic wrote on stderr, which ExecRunner folds
// into msg after the command line, so each line is read from its last message.
func restoreErrors(msg string) []string {
	var paths []string
	for _, line := range strings.Split(msg, "\n") {
		i := strings.LastIndex(line, `{"message_type"`)
		var l restoreLine
		if i < 0 || json.Unmarshal([]byte(line[i:]), &l) != nil || l.MessageType != "error" || l.Item == "" {
			continue
		}
		if paths = append(paths, l.Item); len(paths) == maxUnreadablePaths {
			break
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

// Dump returns one file of snapshot id, ErrNotInSnapshot when the snapshot lacks it.
func (c *Client) Dump(ctx context.Context, id, path string) (string, error) {
	out, err := c.run(ctx, "dump", id, path)
	if err != nil && notInSnapshot.MatchString(err.Error()) {
		return "", fmt.Errorf("%w: %w", ErrNotInSnapshot, err)
	}
	return out, err
}
