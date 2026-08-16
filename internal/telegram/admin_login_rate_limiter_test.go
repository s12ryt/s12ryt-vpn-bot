package telegram

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdminLoginRateLimiterAllowsThreeAttemptsPerMinuteAtExactBoundary(t *testing.T) {
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	limiter := NewAdminLoginRateLimiter(func() time.Time { return now })

	for attempt := 1; attempt <= 3; attempt++ {
		if !limiter.Allow(12345) {
			t.Fatalf("Allow() attempt %d = false, want true", attempt)
		}
	}
	if limiter.Allow(12345) {
		t.Fatal("Allow() fourth attempt = true, want false")
	}
	if !limiter.Allow(67890) {
		t.Fatal("Allow() for another Telegram ID = false, want true")
	}

	now = now.Add(time.Minute)
	if !limiter.Allow(12345) {
		t.Fatal("Allow() at exact window boundary = false, want true")
	}
}

func TestAdminLoginRateLimiterDoesNotExceedLimitConcurrently(t *testing.T) {
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	limiter := NewAdminLoginRateLimiter(func() time.Time { return now })
	start := make(chan struct{})
	var allowed atomic.Int32
	var group sync.WaitGroup

	for range 20 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			if limiter.Allow(12345) {
				allowed.Add(1)
			}
		}()
	}
	close(start)
	group.Wait()

	if got := allowed.Load(); got != 3 {
		t.Fatalf("allowed attempts = %d, want 3", got)
	}
}

func TestAdminLoginRateLimiterRejectsInvalidTelegramID(t *testing.T) {
	limiter := NewAdminLoginRateLimiter(time.Now)
	if limiter.Allow(0) {
		t.Fatal("Allow(0) = true, want false")
	}
}
