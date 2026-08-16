package qualification

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
)

const maxMembershipAttempts = 5

type RetryWaitFunc func(ctx context.Context, duration time.Duration) error
type JitterFunc func(maximum time.Duration) time.Duration

type RetryingMemberLookup struct {
	delegate MemberLookup
	interval time.Duration
	wait     RetryWaitFunc
	now      func() time.Time
	jitter   JitterFunc

	mu          sync.Mutex
	nextRequest time.Time
}

func NewRetryingMemberLookup(
	delegate MemberLookup,
	requestsPerSecond int,
	wait RetryWaitFunc,
	now func() time.Time,
	jitter JitterFunc,
) (*RetryingMemberLookup, error) {
	if delegate == nil {
		return nil, errors.New("member lookup is required")
	}
	if requestsPerSecond < 1 || requestsPerSecond > 20 {
		return nil, errors.New("membership request rate must be between 1 and 20 per second")
	}
	if wait == nil {
		wait = waitForRetry
	}
	if now == nil {
		now = time.Now
	}
	if jitter == nil {
		jitter = cryptoJitter
	}
	return &RetryingMemberLookup{
		delegate: delegate,
		interval: time.Second / time.Duration(requestsPerSecond),
		wait:     wait,
		now:      now,
		jitter:   jitter,
	}, nil
}

func (lookup *RetryingMemberLookup) GetChatMember(ctx context.Context, chatID, userID int64) (telegram.ChatMember, error) {
	if lookup == nil || lookup.delegate == nil {
		return telegram.ChatMember{}, errors.New("retrying member lookup is not configured")
	}
	for attempt := 0; attempt < maxMembershipAttempts; attempt++ {
		if err := lookup.waitForRate(ctx); err != nil {
			return telegram.ChatMember{}, err
		}
		member, err := lookup.delegate.GetChatMember(ctx, chatID, userID)
		if err == nil {
			return member, nil
		}
		if !telegram.IsTemporary(err) || attempt == maxMembershipAttempts-1 {
			return telegram.ChatMember{}, err
		}
		delay, ok := telegram.RetryAfter(err)
		if !ok {
			delay = time.Second << attempt
			maximumJitter := delay / 2
			jitter := lookup.jitter(maximumJitter)
			if jitter > 0 && jitter < maximumJitter {
				delay += jitter
			}
		}
		if err := lookup.wait(ctx, delay); err != nil {
			return telegram.ChatMember{}, err
		}
	}
	return telegram.ChatMember{}, errors.New("membership retry attempts exhausted")
}

func (lookup *RetryingMemberLookup) waitForRate(ctx context.Context) error {
	lookup.mu.Lock()
	now := lookup.now()
	requestAt := now
	if lookup.nextRequest.After(requestAt) {
		requestAt = lookup.nextRequest
	}
	lookup.nextRequest = requestAt.Add(lookup.interval)
	lookup.mu.Unlock()
	if delay := requestAt.Sub(now); delay > 0 {
		return lookup.wait(ctx, delay)
	}
	return nil
}

func waitForRetry(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func cryptoJitter(maximum time.Duration) time.Duration {
	if maximum <= 1 {
		return 0
	}
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(maximum)))
	if err != nil {
		return 0
	}
	return time.Duration(value.Int64())
}
