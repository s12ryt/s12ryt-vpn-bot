package tlsrunner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/acme"
)

func TestCoordinatorRunExecutesImmediatelyAndStopsOnCancel(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	loader := &loaderStub{settings: duckdnsSettings()}
	obtainer := &obtainerStub{err: errors.New("issuance down")}
	failures := &failureRecorderStub{}
	coordinator, err := NewCoordinator(loader, obtainer, &issuanceRecorderStub{}, failures, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	waits := make(chan time.Duration, 4)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-waits
		cancel()
	}()
	wait := func(_ context.Context, duration time.Duration) error {
		waits <- duration
		<-ctx.Done()
		return ctx.Err()
	}

	if err := coordinator.Run(ctx, wait); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(obtainer.calls) == 0 || failures.calls == 0 {
		t.Fatalf("issuance attempt=%d failures=%d, want at least one attempt recorded as failure", len(obtainer.calls), failures.calls)
	}
	if failures.reason != "all_cas_failed" {
		t.Fatalf("failure reason = %q", failures.reason)
	}
}

func TestCoordinatorRunReturnsNilOnImmediateCancel(t *testing.T) {
	coordinator, err := NewCoordinator(&loaderStub{err: acme.ErrNotConfigured}, &obtainerStub{}, &issuanceRecorderStub{}, &failureRecorderStub{}, time.Now)
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := coordinator.Run(ctx, func(context.Context, time.Duration) error { return context.Canceled }); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}
