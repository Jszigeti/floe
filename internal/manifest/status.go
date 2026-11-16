package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type status struct {
	Failed map[string]time.Time `json:"failed"` // collector → first failure in a row
}

// Failures reads status.json: each failing collector and when it started
// failing. No status yet means nothing failed.
func Failures(dir string) (map[string]time.Time, error) {
	data, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]time.Time{}, nil
	}
	if err != nil {
		return nil, err
	}
	var s status
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Failed == nil {
		s.Failed = map[string]time.Time{}
	}
	return s.Failed, nil
}

// recordFailures keeps the first failure date of a collector still failing and drops the ones that succeeded.
func recordFailures(dir string, failed map[string]bool, now time.Time) error {
	previous, _ := Failures(dir) // an unreadable status restarts every date from now
	s := status{Failed: map[string]time.Time{}}
	for name := range failed {
		since, ok := previous[name]
		if !ok {
			since = now
		}
		s.Failed[name] = since.UTC().Truncate(time.Second)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "status.json"), append(data, '\n'), 0o644)
}

// Incomplete says which collectors fail and since when, "" when none does.
func Incomplete(failed map[string]time.Time) string {
	if len(failed) == 0 {
		return ""
	}
	names := slices.Sorted(maps.Keys(failed))
	since := slices.MinFunc(slices.Collect(maps.Values(failed)), time.Time.Compare)
	return fmt.Sprintf("recipe incomplete since %s: %s failed", since.Local().Format("2006-01-02"), strings.Join(names, ", "))
}
