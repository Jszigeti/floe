package restic

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/Jszigeti/floe/internal/config"
)

type Snapshot struct {
	Time    time.Time `json:"time"`
	ShortID string    `json:"short_id"`
	Paths   []string  `json:"paths"`
	Tags    []string  `json:"tags"`
}

func (c *Client) Snapshots(ctx context.Context) ([]Snapshot, error) {
	out, err := c.run(ctx, "snapshots", "--json")
	if err != nil {
		return nil, err
	}
	var snaps []Snapshot
	if err := json.Unmarshal([]byte(out), &snaps); err != nil {
		return nil, fmt.Errorf("parsing snapshots: %w", err)
	}
	return snaps, nil
}

// Forget groups snapshots by tags only: a repository belongs to one Mac at a time, so
// snapshots taken under an older name age out with the rest, while each kind ages on its own.
func (c *Client) Forget(ctx context.Context, r config.Retention) error {
	_, err := c.run(ctx, "forget", "--prune", "--group-by", "tags", "--keep-tag", "replaced",
		"--keep-daily", strconv.Itoa(r.Daily),
		"--keep-weekly", strconv.Itoa(r.Weekly),
		"--keep-monthly", strconv.Itoa(r.Monthly))
	return err
}

func (c *Client) Restore(ctx context.Context, id, target, include string) error {
	args := []string{"restore", id, "--target", target}
	if include != "" {
		args = append(args, "--include", include)
	}
	_, err := c.run(ctx, args...)
	return err
}

// Unlock removes stale locks only — safe to run before every backup.
func (c *Client) Unlock(ctx context.Context) error {
	_, err := c.run(ctx, "unlock")
	return err
}

func (c *Client) RepoSize(ctx context.Context) (int64, error) {
	out, err := c.run(ctx, "stats", "--mode", "raw-data", "--json")
	if err != nil {
		return 0, err
	}
	return parseTotalSize(out)
}
