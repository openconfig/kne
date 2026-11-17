package grpcwire

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLogLimiter(t *testing.T) {
	l := newLogLimiter(10 * time.Second)
	start := time.Unix(1000, 0)

	steps := []struct {
		desc           string
		at             time.Duration
		wantOK         bool
		wantSuppressed uint64
	}{
		{desc: "first event", at: 0, wantOK: true},
		{desc: "within interval", at: time.Second, wantOK: false},
		{desc: "still within interval", at: 9 * time.Second, wantOK: false},
		{desc: "interval elapsed", at: 10 * time.Second, wantOK: true, wantSuppressed: 2},
		{desc: "within next interval", at: 15 * time.Second, wantOK: false},
		{desc: "long gap", at: time.Hour, wantOK: true, wantSuppressed: 1},
		{desc: "right after long gap", at: time.Hour + time.Millisecond, wantOK: false},
	}
	for _, s := range steps {
		ok, suppressed := l.allow(start.Add(s.at))
		if ok != s.wantOK || suppressed != s.wantSuppressed {
			t.Errorf("%s: allow(+%v) = (%v, %d), want (%v, %d)", s.desc, s.at, ok, suppressed, s.wantOK, s.wantSuppressed)
		}
	}
}

func TestLogLimiterConcurrent(t *testing.T) {
	l := newLogLimiter(time.Hour)
	now := time.Unix(1000, 0)

	const goroutines, perGoroutine = 8, 1000
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perGoroutine {
				if ok, _ := l.allow(now); ok {
					allowed.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	if got := allowed.Load(); got != 1 {
		t.Errorf("allowed %d events within one interval, want 1", got)
	}
	ok, suppressed := l.allow(now.Add(time.Hour))
	if want := uint64(goroutines*perGoroutine - 1); !ok || suppressed != want {
		t.Errorf("allow after interval = (%v, %d), want (true, %d)", ok, suppressed, want)
	}
}
