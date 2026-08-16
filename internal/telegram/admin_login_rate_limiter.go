package telegram

import (
	"sync"
	"time"
)

const (
	adminLoginRateLimit  = 3
	adminLoginRateWindow = time.Minute
)

type AdminLoginRateLimiter struct {
	mu       sync.Mutex
	now      func() time.Time
	attempts map[int64][]time.Time
}

func NewAdminLoginRateLimiter(now func() time.Time) *AdminLoginRateLimiter {
	if now == nil {
		now = time.Now
	}
	return &AdminLoginRateLimiter{
		now:      now,
		attempts: make(map[int64][]time.Time),
	}
}

func (limiter *AdminLoginRateLimiter) Allow(telegramID int64) bool {
	if limiter == nil || telegramID <= 0 {
		return false
	}

	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	now := limiter.now()
	cutoff := now.Add(-adminLoginRateWindow)
	previous := limiter.attempts[telegramID]
	kept := previous[:0]
	for _, attemptedAt := range previous {
		if attemptedAt.After(cutoff) {
			kept = append(kept, attemptedAt)
		}
	}
	if len(kept) >= adminLoginRateLimit {
		limiter.attempts[telegramID] = kept
		return false
	}
	limiter.attempts[telegramID] = append(kept, now)
	return true
}
