package qualification

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestRecheckSchedulerRunsImmediatelyAndUsesLatestInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cycle := &scheduledCycleStub{intervals: []time.Duration{time.Hour, 2 * time.Hour}}
	waiter := &scheduleWaitStub{cancelAfter: 2, cancel: cancel}
	scheduler, err := NewRecheckScheduler(cycle, waiter.Wait)
	if err != nil {
		t.Fatalf("NewRecheckScheduler() error = %v", err)
	}

	if err := scheduler.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if cycle.calls != 2 || !reflect.DeepEqual(waiter.durations, []time.Duration{time.Hour, 2 * time.Hour}) {
		t.Fatalf("calls=%d waits=%v", cycle.calls, waiter.durations)
	}
}

func TestRecheckSchedulerRetriesFailedCycleWithoutBusyLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cycle := &scheduledCycleStub{
		intervals: []time.Duration{0, time.Hour},
		errors:    []error{errors.New("settings unavailable")},
	}
	waiter := &scheduleWaitStub{cancelAfter: 2, cancel: cancel}
	scheduler, err := NewRecheckScheduler(cycle, waiter.Wait)
	if err != nil {
		t.Fatalf("NewRecheckScheduler() error = %v", err)
	}

	if err := scheduler.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if cycle.calls != 2 || !reflect.DeepEqual(waiter.durations, []time.Duration{time.Minute, time.Hour}) {
		t.Fatalf("calls=%d waits=%v", cycle.calls, waiter.durations)
	}
}

func TestRecheckSchedulerTriggerInterruptsWaitAndCoalesces(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cycle := &scheduledCycleStub{intervals: []time.Duration{time.Hour, time.Hour}}
	var scheduler *RecheckScheduler
	waits := 0
	wait := func(waitCtx context.Context, _ time.Duration) error {
		waits++
		if waits == 1 {
			scheduler.Trigger()
			scheduler.Trigger()
			return waitCtx.Err()
		}
		cancel()
		return context.Canceled
	}
	var err error
	scheduler, err = NewRecheckScheduler(cycle, wait)
	if err != nil {
		t.Fatalf("NewRecheckScheduler() error = %v", err)
	}

	if err := scheduler.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if cycle.calls != 2 || waits != 2 {
		t.Fatalf("cycle calls=%d waits=%d, want one triggered extra run", cycle.calls, waits)
	}
}

type scheduledCycleStub struct {
	intervals []time.Duration
	errors    []error
	calls     int
}

func (stub *scheduledCycleStub) RunOnce(context.Context) (RecheckSummary, time.Duration, error) {
	index := stub.calls
	stub.calls++
	var interval time.Duration
	if index < len(stub.intervals) {
		interval = stub.intervals[index]
	}
	var err error
	if index < len(stub.errors) {
		err = stub.errors[index]
	}
	return RecheckSummary{}, interval, err
}

type scheduleWaitStub struct {
	durations   []time.Duration
	cancelAfter int
	cancel      context.CancelFunc
}

func (stub *scheduleWaitStub) Wait(ctx context.Context, duration time.Duration) error {
	stub.durations = append(stub.durations, duration)
	if len(stub.durations) >= stub.cancelAfter {
		stub.cancel()
		return ctx.Err()
	}
	return nil
}
