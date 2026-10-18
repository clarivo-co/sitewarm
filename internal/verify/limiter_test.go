package verify

import (
	"context"
	"testing"
	"time"
)

func TestLimiterWaitSpacesRequests(t *testing.T) {
	l := NewLimiter(20) // 50ms apart
	start := time.Now()
	for range 4 {
		if err := l.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(start); d < 140*time.Millisecond {
		t.Fatalf("4 requests at 20 rps took %v, want >= 150ms", d)
	}
}

// Take (the scheduler) and Wait (sitemap and robots.txt fetches) draw from
// the same budget.
func TestLimiterSharedBetweenTakeAndWait(t *testing.T) {
	l := NewLimiter(10) // 100ms apart
	now := time.Now()
	l.Take(now)
	if next := l.Next(); next.Sub(now) != 100*time.Millisecond {
		t.Fatalf("Next = now+%v, want +100ms", next.Sub(now))
	}
	start := time.Now()
	if err := l.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 80*time.Millisecond {
		t.Fatalf("Wait after Take returned after %v", d)
	}
}

func TestLimiterUnlimitedAndCanceled(t *testing.T) {
	var nilLimiter *Limiter
	nilLimiter.Take(time.Now())
	if err := nilLimiter.Wait(context.Background()); err != nil || !nilLimiter.Next().IsZero() {
		t.Fatal("a nil limiter must be unlimited")
	}
	l := NewLimiter(0)
	for range 100 {
		l.Take(time.Now())
	}
	if !l.Next().IsZero() {
		t.Fatal("rps 0 must be unlimited")
	}
	slow := NewLimiter(1)
	slow.Take(time.Now())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := slow.Wait(ctx); err == nil {
		t.Fatal("Wait must return the context error")
	}
}
