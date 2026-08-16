package trafficrunner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/postgres"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/trafficstats"
)

type FailureStage string

const (
	FailureCollect FailureStage = "collect"
	FailureSpool   FailureStage = "spool"
	FailureRecord  FailureStage = "record"
	FailureCleanup FailureStage = "cleanup"
)

type Collector interface {
	Collect(context.Context) ([]trafficstats.Sample, error)
}

type Spool interface {
	Load(context.Context) (trafficstats.PendingBatch, bool, error)
	Save(context.Context, trafficstats.PendingBatch) error
	Delete(context.Context) error
}

type Recorder interface {
	RecordPendingBatch(context.Context, trafficstats.PendingBatch) (postgres.TrafficBatchResult, error)
}

type Result struct {
	Replayed            bool
	Applied             int
	RevokedTelegramIDs  []int64
	RestoredTelegramIDs []int64
}

type Runner struct {
	collector Collector
	spool     Spool
	recorder  Recorder
}

type stepError struct {
	stage FailureStage
	cause error
}

func (err *stepError) Error() string {
	return fmt.Sprintf("traffic %s failed", err.stage)
}

func (err *stepError) Unwrap() error {
	return err.cause
}

func New(collector Collector, spool Spool, recorder Recorder) *Runner {
	return &Runner{collector: collector, spool: spool, recorder: recorder}
}

func (runner *Runner) Step(ctx context.Context, now time.Time) (Result, error) {
	if runner == nil || runner.collector == nil || runner.spool == nil || runner.recorder == nil {
		return Result{}, errors.New("traffic runner dependencies are required")
	}
	if now.IsZero() {
		return Result{}, errors.New("traffic collection time is required")
	}

	batch, exists, err := runner.spool.Load(ctx)
	if err != nil {
		return Result{}, fail(FailureSpool, err)
	}
	if exists {
		return runner.commit(ctx, batch, true)
	}

	samples, err := runner.collector.Collect(ctx)
	if err != nil {
		return Result{}, fail(FailureCollect, err)
	}
	if len(samples) == 0 {
		return Result{}, nil
	}
	batch, err = trafficstats.NewPendingBatch(now, samples)
	if err != nil {
		return Result{}, fail(FailureSpool, err)
	}
	if err := runner.spool.Save(ctx, batch); err != nil {
		return Result{}, fail(FailureSpool, err)
	}
	return runner.commit(ctx, batch, false)
}

func (runner *Runner) commit(ctx context.Context, batch trafficstats.PendingBatch, replayed bool) (Result, error) {
	recorded, err := runner.recorder.RecordPendingBatch(ctx, batch)
	if err != nil {
		return Result{}, fail(FailureRecord, err)
	}
	if err := runner.spool.Delete(ctx); err != nil {
		return Result{}, fail(FailureCleanup, err)
	}
	return Result{
		Replayed:            replayed,
		Applied:             recorded.Applied,
		RevokedTelegramIDs:  append([]int64(nil), recorded.RevokedTelegramIDs...),
		RestoredTelegramIDs: append([]int64(nil), recorded.RestoredTelegramIDs...),
	}, nil
}

func fail(stage FailureStage, cause error) error {
	return &stepError{stage: stage, cause: cause}
}

func FailureStageOf(err error) FailureStage {
	var failure *stepError
	if errors.As(err, &failure) {
		return failure.stage
	}
	return ""
}
