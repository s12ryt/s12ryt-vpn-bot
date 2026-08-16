package trafficrunner

import (
	"context"
	"errors"
	"time"
)

type TrafficStepper interface {
	Step(context.Context, time.Time) (Result, error)
}

type FaultObservation struct {
	Notify     bool
	FailClosed bool
	StartedAt  time.Time
}

type FaultRecovery struct {
	Recovered     bool
	WasFailClosed bool
	StartedAt     time.Time
}

type FaultNotification struct {
	Stage      FailureStage
	FailClosed bool
	StartedAt  time.Time
}

type FaultHealthStore interface {
	ObserveFailure(context.Context, FailureStage, time.Time) (FaultObservation, error)
	ObserveRecovery(context.Context, time.Time) (FaultRecovery, error)
}

type FaultNotifier interface {
	NotifyFailure(context.Context, FaultNotification) error
	NotifyRecovery(context.Context, FaultRecovery) error
}

type Service struct {
	stepper  TrafficStepper
	health   FaultHealthStore
	notifier FaultNotifier
}

type WaitFunc func(context.Context, time.Duration) error

func NewService(stepper TrafficStepper, health FaultHealthStore, notifier FaultNotifier) *Service {
	return &Service{stepper: stepper, health: health, notifier: notifier}
}

func (service *Service) Step(ctx context.Context, now time.Time) error {
	if service == nil || service.stepper == nil || service.health == nil || service.notifier == nil {
		return errors.New("traffic service dependencies are required")
	}
	if now.IsZero() {
		return errors.New("traffic service time is required")
	}
	_, stepErr := service.stepper.Step(ctx, now)
	if stepErr != nil {
		stage := FailureStageOf(stepErr)
		if !validFailureStage(stage) {
			stage = FailureRecord
		}
		observation, healthErr := service.health.ObserveFailure(ctx, stage, now)
		var notifyErr error
		if healthErr == nil && observation.Notify {
			notifyErr = service.notifier.NotifyFailure(context.WithoutCancel(ctx), FaultNotification{
				Stage: stage, FailClosed: observation.FailClosed, StartedAt: observation.StartedAt,
			})
		}
		return errors.Join(stepErr, healthErr, notifyErr)
	}

	recovery, err := service.health.ObserveRecovery(ctx, now)
	if err != nil {
		return err
	}
	if recovery.Recovered {
		return service.notifier.NotifyRecovery(context.WithoutCancel(ctx), recovery)
	}
	return nil
}

func (service *Service) Run(ctx context.Context, wait WaitFunc) error {
	if wait == nil {
		wait = func(ctx context.Context, duration time.Duration) error {
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		_ = service.Step(ctx, time.Now())
		if err := wait(ctx, 15*time.Second); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

func validFailureStage(stage FailureStage) bool {
	switch stage {
	case FailureCollect, FailureSpool, FailureRecord, FailureCleanup:
		return true
	default:
		return false
	}
}
