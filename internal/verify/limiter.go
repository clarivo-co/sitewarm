package verify

import (
	"context"
	"sync"
	"time"
)

// Limiter spaces requests to a global rate. One Limiter is shared by every
// request of a run: sitemap and robots.txt fetches, first attempts and
// retries. It is safe for concurrent use; a nil Limiter is unlimited.
type Limiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time // earliest time the next request may start
}

// NewLimiter returns a limiter for rps requests per second; zero or less
// means unlimited.
func NewLimiter(rps float64) *Limiter {
	l := &Limiter{}
	if rps > 0 {
		l.interval = time.Duration(float64(time.Second) / rps)
	}
	return l
}

// Next is the earliest time the next request may start.
func (l *Limiter) Next() time.Time {
	if l == nil {
		return time.Time{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.next
}

// Take records a request starting at now.
func (l *Limiter) Take(now time.Time) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.take(now)
}

func (l *Limiter) take(now time.Time) time.Time {
	if l.interval <= 0 {
		return now
	}
	start := l.next
	if start.Before(now) {
		start = now
	}
	l.next = start.Add(l.interval)
	return start
}

// Wait reserves the next slot and blocks until it starts or ctx ends.
func (l *Limiter) Wait(ctx context.Context) error {
	if l == nil {
		return ctx.Err()
	}
	l.mu.Lock()
	start := l.take(time.Now())
	l.mu.Unlock()
	d := time.Until(start)
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
