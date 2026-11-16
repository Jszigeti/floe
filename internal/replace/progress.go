package replace

import (
	"time"

	"github.com/Jszigeti/floe/internal/restic"
)

// Stall tells how long a counter of work done has not moved. Now is the clock.
type Stall struct {
	After time.Duration
	Now   func() time.Time
	last  int64
	since time.Time
}

// Seen records progress, the work done so far, and returns how long it has not moved once that
// reaches After, else 0. The first call starts the clock.
func (s *Stall) Seen(progress int64) (stalledFor time.Duration) {
	now := s.Now()
	if s.since.IsZero() || progress != s.last {
		s.last, s.since = progress, now
		return 0
	}
	if d := now.Sub(s.since); d >= s.After {
		return d
	}
	return 0
}

// SeenRestore is Seen for a restore: while restic loads the backup's index nothing moves, which is
// no stall, and the wait starts over once files come.
func (s *Stall) SeenRestore(st restic.RestoreStatus) (stalledFor time.Duration) {
	if st.Loading {
		s.since = time.Time{}
		return 0
	}
	return s.Seen(st.Done)
}
