package icloud

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Jszigeti/floe/internal/run"
)

func IsICloudPath(path string) bool {
	return strings.Contains(path, "/Library/Mobile Documents")
}

func QuotaRemaining(ctx context.Context, r run.Runner) (int64, error) {
	out, err := r.Run(ctx, run.Cmd{Name: "brctl", Args: []string{"quota"}})
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return 0, fmt.Errorf("unexpected brctl quota output: %q", out)
	}
	n, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("unexpected brctl quota output: %q", out)
	}
	return n, nil
}

func SyncPending(ctx context.Context, r run.Runner) (bool, error) {
	out, err := r.Run(ctx, run.Cmd{Name: "brctl", Args: []string{"status", "com.apple.CloudDocs"}})
	if err != nil {
		return false, err
	}
	return strings.Contains(out, "needs-upload"), nil
}

func OptimizeStorageEnabled(ctx context.Context, r run.Runner) bool {
	out, err := r.Run(ctx, run.Cmd{Name: "defaults", Args: []string{"read", "com.apple.bird", "optimize-storage"}})
	return err == nil && strings.TrimSpace(out) == "1"
}
