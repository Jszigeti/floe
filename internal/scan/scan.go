package scan

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

type DirSize struct {
	Path  string
	Bytes int64
}

func Excluded(path string, excludes []string) bool {
	base := filepath.Base(path)
	for _, e := range excludes {
		if strings.ContainsRune(e, '/') {
			if path == e || strings.HasPrefix(path, e+"/") {
				return true
			}
		} else if base == e {
			return true
		}
	}
	return false
}

func Estimate(root string, excludes []string) (int64, []DirSize, error) {
	var total int64
	perChild := map[string]int64{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		if Excluded(p, excludes) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		total += info.Size()
		if rel, rerr := filepath.Rel(root, p); rerr == nil {
			child := strings.SplitN(rel, string(filepath.Separator), 2)[0]
			if child != rel { // file is inside a first-level directory
				perChild[filepath.Join(root, child)] += info.Size()
			}
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	top := make([]DirSize, 0, len(perChild))
	for p, b := range perChild {
		top = append(top, DirSize{Path: p, Bytes: b})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].Bytes > top[j].Bytes })
	return total, top, nil
}
