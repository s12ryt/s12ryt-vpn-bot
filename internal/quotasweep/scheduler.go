package quotasweep

import (
	"context"
	"errors"
	"time"
)

const (
	sweepInterval = time.Minute
	sweepBatch    = 1000
)

type Sweeper interface {
	AdvanceDueQuotaPeriods(context.Context, time.Time, int) (int64, int64, error)
}

type WaitFunc func(context.Context, time.Duration) error

type Scheduler struct {
	sweeper Sweeper
	wait    WaitFunc
	now     func() time.Time
}

func New(sweeper Sweeper, wait WaitFunc, now func() time.Time) (*Scheduler, error) {
	if sweeper == nil {
		return nil, errors.New("quota sweeper is required")
	}
	if wait == nil {
		wait = defaultWait
	}
	if now == nil {
		now = time.Now
	}
	return &Scheduler{sweeper: sweeper, wait: wait, now: now}, nil
}

func (scheduler *Scheduler) Run(ctx context.Context) error {
	if scheduler == nil || scheduler.sweeper == nil || scheduler.wait == nil || scheduler.now == nil {
		return errors.New("quota scheduler is not initialized")
	}
	for {
		_, _, _ = scheduler.sweeper.AdvanceDueQuotaPeriods(ctx, scheduler.now(), sweepBatch)
		if err := scheduler.wait(ctx, sweepInterval); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			return err
		}
	}
}

func defaultWait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
