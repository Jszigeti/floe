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
	total int64 // the restore's last total
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
// no stall, and the wait starts over once files come. A total that grows is restic walking the
// backup's tree before it restores anything: a sign of life too.
func (s *Stall) SeenRestore(st restic.RestoreStatus) (stalledFor time.Duration) {
	if st.Loading || st.Total != s.total {
		s.since, s.total = time.Time{}, st.Total
	}
	if st.Loading {
		return 0
	}
	return s.Seen(st.Done)
}
