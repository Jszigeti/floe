package restic

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/Jszigeti/floe/internal/run"
)

// Entry is one node of a snapshot listing. Type is "file", "dir" or "symlink".
type Entry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

type lsLine struct {
	StructType string `json:"struct_type"`
	Entry
}

// List calls each for every node under path in snapshot id, in restic's order, as restic prints them.
// Lines that are not nodes (the leading snapshot line) or do not parse are skipped.
func (c *Client) List(ctx context.Context, id, path string, each func(Entry)) error {
	w := &lineWriter{each: func(line []byte) {
		var l lsLine
		if json.Unmarshal(line, &l) == nil && l.StructType == "node" {
			each(l.Entry)
		}
	}}
	_, err := c.R.Run(ctx, run.Cmd{
		Name:   c.Bin,
		Args:   []string{"ls", "--recursive", "--json", id, path},
		Env:    c.env(),
		Stdout: w,
	})
	if len(w.rest) > 0 {
		w.each(w.rest)
	}
	return err
}

// lineWriter hands each complete line to each and keeps only the unfinished tail.
type lineWriter struct {
	rest []byte
	each func([]byte)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	lines := bytes.Split(append(w.rest, p...), []byte("\n"))
	for _, line := range lines[:len(lines)-1] {
		w.each(line)
	}
	w.rest = bytes.Clone(lines[len(lines)-1])
	return len(p), nil
}
