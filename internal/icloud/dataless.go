package icloud

import (
	"context"
	"io/fs"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Jszigeti/floe/internal/run"
)

const sfDataless = 0x40000000

type FlagsFn func(path string) (uint32, error)

func StatFlags(path string) (uint32, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return 0, err
	}
	return st.Flags, nil
}

func DatalessFiles(root string, fn FlagsFn) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		flags, ferr := fn(p)
		if ferr == nil && flags&sfDataless != 0 {
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

func Materialize(ctx context.Context, r run.Runner, fn FlagsFn, paths []string) error {
	for _, p := range paths {
		if _, err := r.Run(ctx, run.Cmd{Name: "brctl", Args: []string{"download", p}}); err != nil {
			return err
		}
	}
	// Known ceiling: stat errors count as "not dataless", so a failed poll can report success early — restic surfaces the real failure on next read.
	for {
		remaining := 0
		for _, p := range paths {
			if flags, err := fn(p); err == nil && flags&sfDataless != 0 {
				remaining++
			}
		}
		if remaining == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
