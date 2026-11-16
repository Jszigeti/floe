package cli

import (
	"context"
	"fmt"
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

// pinSnapshot is the snapshot a resumed Replace started from, else b's newest home snapshot,
// recorded so that an interrupted run resumes on it.
func pinSnapshot(ctx context.Context, client *restic.Client, b foundBackup, resume replace.State) (replace.State, error) {
	if resume.SnapshotID != "" {
		return resume, nil
	}
	snap, err := client.Latest(ctx, "home")
	if err != nil {
		return replace.State{}, err
	}
	if len(snap.Paths) != 1 {
		return replace.State{}, fmt.Errorf("snapshot %s covers %d folders, not one home folder: restore it with floe restore", snap.ShortID, len(snap.Paths))
	}
	st := replace.State{Backup: b.Name, Repo: b.Path, SnapshotID: snap.ShortID, OldHome: snap.Paths[0], Started: time.Now()}
	return st, st.Save()
}
