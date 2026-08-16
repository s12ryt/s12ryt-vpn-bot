package trafficrunner

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServicePersistsFailuresAndNotifiesOnlyWhenRequested(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	stepper := &trafficStepperStub{err: fail(FailureRecord, errors.New("database unavailable"))}
	health := &trafficHealthStub{failure: FaultObservation{Notify: true, FailClosed: false, StartedAt: now}}
	notifier := &trafficNotifierStub{}
	service := NewService(stepper, health, notifier)

	if err := service.Step(context.Background(), now); err == nil {
		t.Fatal("Step() error = nil")
	}
	if health.failureStage != FailureRecord || !health.failureAt.Equal(now) {
		t.Fatalf("failure observation = %q %v", health.failureStage, health.failureAt)
	}
	if len(notifier.failures) != 1 || notifier.failures[0].Stage != FailureRecord || notifier.failures[0].FailClosed {
		t.Fatalf("failure notifications = %#v", notifier.failures)
	}
}

func TestServiceFailureCanTransitionToFailClosed(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 5, 0, 0, time.UTC)
	health := &trafficHealthStub{failure: FaultObservation{Notify: true, FailClosed: true, StartedAt: now.Add(-5 * time.Minute)}}
	notifier := &trafficNotifierStub{}
	service := NewService(&trafficStepperStub{err: fail(FailureCollect, errors.New("rpc unavailable"))}, health, notifier)

	if err := service.Step(context.Background(), now); err == nil {
		t.Fatal("Step() error = nil")
	}
	if len(notifier.failures) != 1 || !notifier.failures[0].FailClosed {
		t.Fatalf("failure notifications = %#v", notifier.failures)
	}
}

func TestServiceRecoveryIsPersistedAndNotified(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 6, 0, 0, time.UTC)
	health := &trafficHealthStub{recovery: FaultRecovery{Recovered: true, WasFailClosed: true, StartedAt: now.Add(-6 * time.Minute)}}
	notifier := &trafficNotifierStub{}
	service := NewService(&trafficStepperStub{}, health, notifier)

	if err := service.Step(context.Background(), now); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if health.recoveryAt != now || len(notifier.recoveries) != 1 || !notifier.recoveries[0].WasFailClosed {
		t.Fatalf("recovery=%#v notifications=%#v", health.recovery, notifier.recoveries)
	}
}

func TestServiceDoesNotReportRecoveryWhenNeverFailed(t *testing.T) {
	notifier := &trafficNotifierStub{}
	service := NewService(&trafficStepperStub{}, &trafficHealthStub{}, notifier)
	if err := service.Step(context.Background(), time.Now()); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if len(notifier.recoveries) != 0 {
		t.Fatalf("recovery notifications = %#v", notifier.recoveries)
	}
}

func TestServiceRunUsesFifteenSecondCadenceAndStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	waits := make([]time.Duration, 0)
	service := NewService(&trafficStepperStub{}, &trafficHealthStub{}, &trafficNotifierStub{})
	err := service.Run(ctx, func(waitCtx context.Context, duration time.Duration) error {
		waits = append(waits, duration)
		cancel()
		return waitCtx.Err()
	})
	if err != nil || len(waits) != 1 || waits[0] != 15*time.Second {
		t.Fatalf("Run() error=%v waits=%v", err, waits)
	}
}

type trafficStepperStub struct{ err error }

func (stub *trafficStepperStub) Step(context.Context, time.Time) (Result, error) {
	return Result{}, stub.err
}

type trafficHealthStub struct {
	failure      FaultObservation
	recovery     FaultRecovery
	failureStage FailureStage
	failureAt    time.Time
	recoveryAt   time.Time
}

func (stub *trafficHealthStub) ObserveFailure(_ context.Context, stage FailureStage, at time.Time) (FaultObservation, error) {
	stub.failureStage, stub.failureAt = stage, at
	return stub.failure, nil
}

func (stub *trafficHealthStub) ObserveRecovery(_ context.Context, at time.Time) (FaultRecovery, error) {
	stub.recoveryAt = at
	return stub.recovery, nil
}

type trafficNotifierStub struct {
	failures   []FaultNotification
	recoveries []FaultRecovery
}

func (stub *trafficNotifierStub) NotifyFailure(_ context.Context, notification FaultNotification) error {
	stub.failures = append(stub.failures, notification)
	return nil
}

func (stub *trafficNotifierStub) NotifyRecovery(_ context.Context, recovery FaultRecovery) error {
	stub.recoveries = append(stub.recoveries, recovery)
	return nil
}
