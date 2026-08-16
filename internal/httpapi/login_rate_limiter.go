package httpapi

import (
	"errors"
	"net/netip"
	"sync"
	"time"
)

type LoginRateLimits struct {
	AccountAttempts int
	AccountWindow   time.Duration
	IPAttempts      int
	IPWindow        time.Duration
	GlobalAttempts  int
	GlobalWindow    time.Duration
}

func DefaultLoginRateLimits() LoginRateLimits {
	return LoginRateLimits{
		AccountAttempts: 5,
		AccountWindow:   15 * time.Minute,
		IPAttempts:      20,
		IPWindow:        15 * time.Minute,
		GlobalAttempts:  100,
		GlobalWindow:    time.Minute,
	}
}

type loginAttemptEvent struct {
	id      uint64
	at      time.Time
	pending bool
}

type LoginRateLimiter struct {
	mutex     sync.Mutex
	limits    LoginRateLimits
	now       func() time.Time
	nextID    uint64
	global    []time.Time
	byIP      map[string][]loginAttemptEvent
	byAccount map[int64][]loginAttemptEvent
}

type LoginAttempt struct {
	limiter    *LoginRateLimiter
	id         uint64
	ip         string
	telegramID int64
	once       sync.Once
}

func NewLoginRateLimiter(limits LoginRateLimits, now func() time.Time) (*LoginRateLimiter, error) {
	if limits.AccountAttempts <= 0 || limits.AccountWindow <= 0 || limits.IPAttempts <= 0 || limits.IPWindow <= 0 || limits.GlobalAttempts <= 0 || limits.GlobalWindow <= 0 {
		return nil, errors.New("login rate limits and windows must be positive")
	}
	if now == nil {
		now = time.Now
	}
	return &LoginRateLimiter{
		limits:    limits,
		now:       now,
		byIP:      make(map[string][]loginAttemptEvent),
		byAccount: make(map[int64][]loginAttemptEvent),
	}, nil
}

func (limiter *LoginRateLimiter) Begin(sourceIP netip.Addr, telegramID int64) (*LoginAttempt, bool) {
	if limiter == nil || !sourceIP.IsValid() || telegramID <= 0 {
		return nil, false
	}
	ip := sourceIP.Unmap().String()
	now := limiter.now()

	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()
	limiter.prune(now)
	if len(limiter.global) >= limiter.limits.GlobalAttempts || len(limiter.byIP[ip]) >= limiter.limits.IPAttempts || len(limiter.byAccount[telegramID]) >= limiter.limits.AccountAttempts {
		return nil, false
	}
	limiter.nextID++
	event := loginAttemptEvent{id: limiter.nextID, at: now, pending: true}
	limiter.global = append(limiter.global, now)
	limiter.byIP[ip] = append(limiter.byIP[ip], event)
	limiter.byAccount[telegramID] = append(limiter.byAccount[telegramID], event)
	return &LoginAttempt{limiter: limiter, id: event.id, ip: ip, telegramID: telegramID}, true
}

func (attempt *LoginAttempt) Complete(succeeded bool) {
	if attempt == nil || attempt.limiter == nil {
		return
	}
	attempt.once.Do(func() {
		attempt.limiter.complete(attempt.id, attempt.ip, attempt.telegramID, succeeded)
	})
}

func (limiter *LoginRateLimiter) complete(id uint64, ip string, telegramID int64, succeeded bool) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()
	if succeeded {
		limiter.byIP[ip] = retainPendingExcept(limiter.byIP[ip], id)
		limiter.byAccount[telegramID] = retainPendingExcept(limiter.byAccount[telegramID], id)
	} else {
		markCompleted(limiter.byIP[ip], id)
		markCompleted(limiter.byAccount[telegramID], id)
	}
	if len(limiter.byIP[ip]) == 0 {
		delete(limiter.byIP, ip)
	}
	if len(limiter.byAccount[telegramID]) == 0 {
		delete(limiter.byAccount, telegramID)
	}
}

func (limiter *LoginRateLimiter) prune(now time.Time) {
	limiter.global = pruneTimes(limiter.global, now.Add(-limiter.limits.GlobalWindow))
	for ip, events := range limiter.byIP {
		events = pruneEvents(events, now.Add(-limiter.limits.IPWindow))
		if len(events) == 0 {
			delete(limiter.byIP, ip)
		} else {
			limiter.byIP[ip] = events
		}
	}
	for telegramID, events := range limiter.byAccount {
		events = pruneEvents(events, now.Add(-limiter.limits.AccountWindow))
		if len(events) == 0 {
			delete(limiter.byAccount, telegramID)
		} else {
			limiter.byAccount[telegramID] = events
		}
	}
}

func pruneTimes(values []time.Time, cutoff time.Time) []time.Time {
	index := 0
	for index < len(values) && !values[index].After(cutoff) {
		index++
	}
	return values[index:]
}

func pruneEvents(events []loginAttemptEvent, cutoff time.Time) []loginAttemptEvent {
	index := 0
	for index < len(events) && !events[index].at.After(cutoff) {
		index++
	}
	return events[index:]
}

func retainPendingExcept(events []loginAttemptEvent, completedID uint64) []loginAttemptEvent {
	kept := events[:0]
	for _, event := range events {
		if event.pending && event.id != completedID {
			kept = append(kept, event)
		}
	}
	return kept
}

func markCompleted(events []loginAttemptEvent, completedID uint64) {
	for index := range events {
		if events[index].id == completedID {
			events[index].pending = false
			return
		}
	}
}
