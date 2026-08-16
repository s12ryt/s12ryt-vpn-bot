package qualification

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
)

func TestRetryingMemberLookupLimitsRequestsToConfiguredRate(t *testing.T) {
	clock := newRetryClock()
	delegate := &sequenceMemberLookup{member: telegram.ChatMember{User: telegram.User{ID: 12345}, Status: "member"}}
	lookup, err := NewRetryingMemberLookup(delegate, 10, clock.Wait, clock.Now, noJitter)
	if err != nil {
		t.Fatalf("NewRetryingMemberLookup() error = %v", err)
	}

	for range 2 {
		if _, err := lookup.GetChatMember(context.Background(), -1001, 12345); err != nil {
			t.Fatalf("GetChatMember() error = %v", err)
		}
	}
	if !reflect.DeepEqual(clock.waits, []time.Duration{100 * time.Millisecond}) {
		t.Fatalf("waits = %v, want [100ms]", clock.waits)
	}
}

func TestRetryingMemberLookupHonorsTelegramRetryAfter(t *testing.T) {
	clock := newRetryClock()
	delegate := &sequenceMemberLookup{
		member: telegram.ChatMember{User: telegram.User{ID: 12345}, Status: "member"},
		errors: []error{&telegram.APIError{
			StatusCode:         429,
			ErrorCode:          429,
			Temporary:          true,
			RetryAfterDuration: 17 * time.Second,
		}},
	}
	lookup, err := NewRetryingMemberLookup(delegate, 10, clock.Wait, clock.Now, noJitter)
	if err != nil {
		t.Fatalf("NewRetryingMemberLookup() error = %v", err)
	}

	if _, err := lookup.GetChatMember(context.Background(), -1001, 12345); err != nil {
		t.Fatalf("GetChatMember() error = %v", err)
	}
	if delegate.calls != 2 || !reflect.DeepEqual(clock.waits, []time.Duration{17 * time.Second}) {
		t.Fatalf("calls=%d waits=%v, want 2 calls and retry_after wait", delegate.calls, clock.waits)
	}
}

func TestRetryingMemberLookupStopsAfterFiveTemporaryAttempts(t *testing.T) {
	clock := newRetryClock()
	temporary := &telegram.APIError{StatusCode: 502, ErrorCode: 502, Temporary: true}
	delegate := &sequenceMemberLookup{errors: []error{temporary, temporary, temporary, temporary, temporary, temporary}}
	lookup, err := NewRetryingMemberLookup(delegate, 10, clock.Wait, clock.Now, noJitter)
	if err != nil {
		t.Fatalf("NewRetryingMemberLookup() error = %v", err)
	}

	if _, err := lookup.GetChatMember(context.Background(), -1001, 12345); !errors.Is(err, temporary) {
		t.Fatalf("GetChatMember() error = %v, want final temporary error", err)
	}
	if delegate.calls != 5 {
		t.Fatalf("calls = %d, want 5", delegate.calls)
	}
	wantWaits := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	if !reflect.DeepEqual(clock.waits, wantWaits) {
		t.Fatalf("waits = %v, want %v", clock.waits, wantWaits)
	}
}

func TestRetryingMemberLookupDoesNotRetryPermanentError(t *testing.T) {
	clock := newRetryClock()
	permanent := &telegram.APIError{StatusCode: 400, ErrorCode: 400}
	delegate := &sequenceMemberLookup{errors: []error{permanent}}
	lookup, err := NewRetryingMemberLookup(delegate, 10, clock.Wait, clock.Now, noJitter)
	if err != nil {
		t.Fatalf("NewRetryingMemberLookup() error = %v", err)
	}

	if _, err := lookup.GetChatMember(context.Background(), -1001, 12345); !errors.Is(err, permanent) {
		t.Fatalf("GetChatMember() error = %v, want permanent error", err)
	}
	if delegate.calls != 1 || len(clock.waits) != 0 {
		t.Fatalf("calls=%d waits=%v, want one call and no retry wait", delegate.calls, clock.waits)
	}
}

type sequenceMemberLookup struct {
	member telegram.ChatMember
	errors []error
	calls  int
}

func (lookup *sequenceMemberLookup) GetChatMember(context.Context, int64, int64) (telegram.ChatMember, error) {
	index := lookup.calls
	lookup.calls++
	if index < len(lookup.errors) && lookup.errors[index] != nil {
		return telegram.ChatMember{}, lookup.errors[index]
	}
	return lookup.member, nil
}

type retryClock struct {
	now   time.Time
	waits []time.Duration
}

func newRetryClock() *retryClock {
	return &retryClock{now: time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)}
}

func (clock *retryClock) Now() time.Time { return clock.now }

func (clock *retryClock) Wait(ctx context.Context, duration time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	clock.waits = append(clock.waits, duration)
	clock.now = clock.now.Add(duration)
	return nil
}

func noJitter(time.Duration) time.Duration { return 0 }
