package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Jszigeti/floe/internal/replace"
	"github.com/Jszigeti/floe/internal/restic"
)

// offerResume asks whether to go on with the Replace an earlier floe init left unfinished; a no
// forgets it, and the wizard starts over.
func offerResume(p prompter) (replace.State, bool, error) {
	st, ok, err := replace.LoadState()
	if err != nil || !ok {
		return replace.State{}, false, err
	}
	if a, _ := p.answer(fmt.Sprintf("Resume replacing %s? [Y/n]", st.Backup)); strings.HasPrefix(strings.ToLower(a), "n") {
		return replace.State{}, false, replace.ClearState()
	}
	return st, true, nil
}

// pinSnapshot is the snapshot a resumed Replace started from, else b's newest home snapshot and
// apps snapshot, recorded so that an interrupted run resumes on them, with what macOS had created in home.
func pinSnapshot(ctx context.Context, client *restic.Client, b foundBackup, resume replace.State, home string) (replace.State, error) {
	if resume.SnapshotID != "" {
		return resume, nil
	}
	snap, err := client.Latest(ctx, "home")
	if err != nil {
		return replace.State{}, err
	}
	appsSnap, err := client.Latest(ctx, "apps")
	if err != nil && !errors.Is(err, restic.ErrNoSnapshot) {
		return replace.State{}, err
	}
	if len(snap.Paths) != 1 {
		return replace.State{}, fmt.Errorf("snapshot %s covers %d folders, not one home folder: restore it with floe restore", snap.ShortID, len(snap.Paths))
	}
	st := replace.State{
		Backup: b.Name, Repo: b.Path, SnapshotID: snap.ShortID, OldHome: snap.Paths[0], Started: time.Now(),
		Present: replace.Present(home), AppsSnapshotID: appsSnap.ShortID,
	}
	return st, st.Save()
}

// pinnedApps is the apps snapshot id names, the zero snapshot when id is empty.
func pinnedApps(ctx context.Context, client *restic.Client, id string) (restic.Snapshot, error) {
	if id == "" {
		return restic.Snapshot{}, nil
	}
	snaps, err := client.Snapshots(ctx)
	if err != nil {
		return restic.Snapshot{}, err
	}
	i := slices.IndexFunc(snaps, func(s restic.Snapshot) bool { return s.ShortID == id })
	if i < 0 {
		return restic.Snapshot{}, fmt.Errorf("apps snapshot %s is no longer in the backup: decline the resume to start over", id)
	}
	return snaps[i], nil
}
