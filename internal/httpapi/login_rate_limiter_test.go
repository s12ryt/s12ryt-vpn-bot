package httpapi

import (
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginRateLimiterEnforcesAccountAndIPSlidingWindows(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	limiter, err := NewLoginRateLimiter(LoginRateLimits{
		AccountAttempts: 2, AccountWindow: 15 * time.Minute,
		IPAttempts: 3, IPWindow: 15 * time.Minute,
		GlobalAttempts: 100, GlobalWindow: time.Minute,
	}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewLoginRateLimiter() error = %v", err)
	}
	ip := netip.MustParseAddr("198.51.100.10")

	for range 2 {
		attempt, allowed := limiter.Begin(ip, 12345)
		if !allowed {
			t.Fatal("attempt unexpectedly rate limited")
		}
		attempt.Complete(false)
	}
	if _, allowed := limiter.Begin(ip, 12345); allowed {
		t.Fatal("third account attempt was allowed inside two-attempt window")
	}
	if attempt, allowed := limiter.Begin(ip, 67890); !allowed {
		t.Fatal("different account should still have one IP attempt available")
	} else {
		attempt.Complete(false)
	}
	if _, allowed := limiter.Begin(ip, 99999); allowed {
		t.Fatal("fourth IP attempt was allowed inside three-attempt window")
	}

	now = now.Add(15 * time.Minute)
	if attempt, allowed := limiter.Begin(ip, 12345); !allowed {
		t.Fatal("attempt at exact sliding-window boundary was not restored")
	} else {
		attempt.Complete(false)
	}
}

func TestLoginRateLimiterSuccessClearsAccountAndIPFailuresButNotGlobalAttempts(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	limiter, err := NewLoginRateLimiter(LoginRateLimits{
		AccountAttempts: 2, AccountWindow: 15 * time.Minute,
		IPAttempts: 2, IPWindow: 15 * time.Minute,
		GlobalAttempts: 3, GlobalWindow: time.Minute,
	}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewLoginRateLimiter() error = %v", err)
	}
	ip := netip.MustParseAddr("198.51.100.10")

	failed, _ := limiter.Begin(ip, 12345)
	failed.Complete(false)
	succeeded, _ := limiter.Begin(ip, 12345)
	succeeded.Complete(true)
	third, allowed := limiter.Begin(ip, 12345)
	if !allowed {
		t.Fatal("success did not clear account and IP failures")
	}
	third.Complete(false)
	if _, allowed := limiter.Begin(netip.MustParseAddr("198.51.100.11"), 67890); allowed {
		t.Fatal("success incorrectly cleared global attempt history")
	}
}

func TestLoginRateLimiterReservesCapacityAcrossConcurrentAttempts(t *testing.T) {
	limiter, err := NewLoginRateLimiter(LoginRateLimits{
		AccountAttempts: 5, AccountWindow: 15 * time.Minute,
		IPAttempts: 20, IPWindow: 15 * time.Minute,
		GlobalAttempts: 100, GlobalWindow: time.Minute,
	}, time.Now)
	if err != nil {
		t.Fatalf("NewLoginRateLimiter() error = %v", err)
	}
	var allowed atomic.Int32
	var wait sync.WaitGroup
	for range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			attempt, ok := limiter.Begin(netip.MustParseAddr("198.51.100.10"), 12345)
			if ok {
				allowed.Add(1)
				attempt.Complete(false)
			}
		}()
	}
	wait.Wait()
	if got := allowed.Load(); got != 5 {
		t.Fatalf("concurrent allowed attempts = %d, want exactly 5", got)
	}
}

func TestLoginRateLimiterRejectsInvalidConfigurationAndIdentity(t *testing.T) {
	if _, err := NewLoginRateLimiter(LoginRateLimits{}, time.Now); err == nil {
		t.Fatal("NewLoginRateLimiter() accepted zero limits")
	}
	limiter, err := NewLoginRateLimiter(DefaultLoginRateLimits(), time.Now)
	if err != nil {
		t.Fatalf("NewLoginRateLimiter() error = %v", err)
	}
	if _, allowed := limiter.Begin(netip.Addr{}, 12345); allowed {
		t.Fatal("Begin() accepted invalid source IP")
	}
	if _, allowed := limiter.Begin(netip.MustParseAddr("198.51.100.10"), 0); allowed {
		t.Fatal("Begin() accepted invalid Telegram ID")
	}
}
