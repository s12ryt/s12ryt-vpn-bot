package qualification

import (
	"context"
	"errors"
	"sync"
	"time"
)

const failedRecheckRetryInterval = time.Minute

type ScheduledRecheckCycle interface {
	RunOnce(ctx context.Context) (RecheckSummary, time.Duration, error)
}

type ScheduleWaitFunc func(ctx context.Context, duration time.Duration) error

type RecheckScheduler struct {
	cycle ScheduledRecheckCycle
	wait  ScheduleWaitFunc

	mu          sync.Mutex
	wakeCancel  context.CancelFunc
	wakePending bool
}

func (scheduler *RecheckScheduler) Trigger() {
	if scheduler == nil {
		return
	}
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	if scheduler.wakeCancel != nil {
		scheduler.wakeCancel()
		return
	}
	scheduler.wakePending = true
}

func NewRecheckScheduler(cycle ScheduledRecheckCycle, wait ScheduleWaitFunc) (*RecheckScheduler, error) {
	if cycle == nil {
		return nil, errors.New("scheduled recheck cycle is required")
	}
	if wait == nil {
		wait = waitForSchedule
	}
	return &RecheckScheduler{cycle: cycle, wait: wait}, nil
}

func (scheduler *RecheckScheduler) Run(ctx context.Context) error {
	if scheduler == nil || scheduler.cycle == nil || scheduler.wait == nil {
		return errors.New("recheck scheduler is not configured")
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		_, interval, _ := scheduler.cycle.RunOnce(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if interval <= 0 {
			interval = failedRecheckRetryInterval
		}
		scheduler.mu.Lock()
		if scheduler.wakePending {
			scheduler.wakePending = false
			scheduler.mu.Unlock()
			continue
		}
		waitContext, cancelWait := context.WithCancel(ctx)
		scheduler.wakeCancel = cancelWait
		scheduler.mu.Unlock()

		err := scheduler.wait(waitContext, interval)
		cancelWait()
		scheduler.mu.Lock()
		scheduler.wakeCancel = nil
		scheduler.mu.Unlock()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, context.Canceled) || waitContext.Err() != nil {
				continue
			}
			return err
		}
	}
}

func waitForSchedule(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
