package grpcwire

import (
	"sync/atomic"
	"time"
)

// logLimiter allows at most one event per interval and counts the events it
// suppresses in between. It is safe for concurrent use and takes no locks,
// so it is cheap enough to call from packet forwarding loops.
type logLimiter struct {
	interval   time.Duration
	next       atomic.Int64 // UnixNano before which events are suppressed
	suppressed atomic.Uint64
}

func newLogLimiter(interval time.Duration) *logLimiter {
	return &logLimiter{interval: interval}
}

// allow reports whether an event at now may be logged. If it may, allow also
// returns how many events were suppressed since the last allowed one. Because
// advancing next and draining suppressed are separate atomic operations,
// concurrent events suppressed right at an interval boundary may be attributed
// to either the triggering call or the next allowed call; the total suppressed
// count is conserved across calls.
func (l *logLimiter) allow(now time.Time) (ok bool, suppressed uint64) {
	n := now.UnixNano()
	next := l.next.Load()
	if n < next || !l.next.CompareAndSwap(next, n+int64(l.interval)) {
		l.suppressed.Add(1)
		return false, 0
	}
	return true, l.suppressed.Swap(0)
}
