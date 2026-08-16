package quotasweep

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSchedulerRunsImmediatelyAndWaitsOneMinute(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sweeper := &sweeperStub{}
	var waits []time.Duration
	scheduler, err := New(sweeper, func(context.Context, time.Duration) error {
		waits = append(waits, time.Minute)
		cancel()
		return context.Canceled
	}, func() time.Time { return time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := scheduler.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if sweeper.calls != 1 || sweeper.limit != 1000 || len(waits) != 1 {
		t.Fatalf("calls=%d limit=%d waits=%v", sweeper.calls, sweeper.limit, waits)
	}
}

func TestSchedulerKeepsRunningAfterSweepFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sweeper := &sweeperStub{err: errors.New("database unavailable")}
	scheduler, err := New(sweeper, func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}, time.Now)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := scheduler.Run(ctx); err != nil || sweeper.calls != 1 {
		t.Fatalf("Run() error=%v calls=%d", err, sweeper.calls)
	}
}

func TestSchedulerRejectsMissingDependencies(t *testing.T) {
	if _, err := New(nil, nil, nil); err == nil {
		t.Fatal("New() error = nil")
	}
}

type sweeperStub struct {
	calls int
	limit int
	err   error
}

func (stub *sweeperStub) AdvanceDueQuotaPeriods(_ context.Context, _ time.Time, limit int) (int64, int64, error) {
	stub.calls++
	stub.limit = limit
	return 0, 0, stub.err
}
