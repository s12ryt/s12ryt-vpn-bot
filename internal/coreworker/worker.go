package coreworker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/postgres"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/singbox"
)

const (
	actionLease         = 2 * time.Minute
	actionBatchLimit    = 1000
	plannedNoticePeriod = 30 * time.Second
	plannedRestartLimit = time.Minute
)

type ActionStore interface {
	ClaimDue(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]postgres.CoreAction, error)
	Complete(ctx context.Context, ids []int64, now time.Time) error
	Retry(ctx context.Context, ids []int64, retryAt time.Time, failure postgres.CoreFailureCode) error
}

type SnapshotBuilder interface {
	Build(ctx context.Context) ([]byte, error)
}

type Installer interface {
	Install(ctx context.Context, configuration []byte) error
}

type Notifier interface {
	NotifyPlannedRestart(ctx context.Context) error
	NotifyCoreFailure(ctx context.Context, failure postgres.CoreFailureCode) error
}

type WaitFunc func(ctx context.Context, delay time.Duration) error

type Worker struct {
	store     ActionStore
	snapshot  SnapshotBuilder
	installer Installer
	notifier  Notifier

	pending            []postgres.CoreAction
	noticeStartedAt    time.Time
	lastPlannedRestart time.Time
	installedAt        time.Time
	installedPlanned   bool
}

func New(store ActionStore, snapshot SnapshotBuilder, installer Installer, notifier Notifier) *Worker {
	return &Worker{store: store, snapshot: snapshot, installer: installer, notifier: notifier}
}

func (worker *Worker) Run(ctx context.Context, wait WaitFunc) error {
	if wait == nil {
		wait = waitForCoreWorker
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		_ = worker.Step(ctx, time.Now())
		if err := wait(ctx, time.Second); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("wait for core worker poll: %w", err)
		}
	}
}

func waitForCoreWorker(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (worker *Worker) Step(ctx context.Context, now time.Time) error {
	if worker == nil || worker.store == nil || worker.snapshot == nil || worker.installer == nil || worker.notifier == nil {
		return errors.New("core worker dependencies are required")
	}
	if now.IsZero() {
		return errors.New("core worker time is required")
	}
	if !worker.installedAt.IsZero() {
		return worker.completeInstalled(ctx, now)
	}
	claimed, err := worker.store.ClaimDue(ctx, now, actionLease, actionBatchLimit)
	if err != nil {
		return fmt.Errorf("claim core actions: %w", err)
	}
	if err := worker.appendClaimed(claimed); err != nil {
		return err
	}
	if len(worker.pending) == 0 {
		return nil
	}

	hasRevoke, hasReconcile := pendingKinds(worker.pending)
	if hasRevoke {
		return worker.deploy(ctx, now, hasReconcile)
	}
	if worker.noticeStartedAt.IsZero() {
		worker.noticeStartedAt = now
		_ = worker.notifier.NotifyPlannedRestart(ctx)
	}
	readyAt := worker.noticeStartedAt.Add(plannedNoticePeriod)
	if !worker.lastPlannedRestart.IsZero() {
		minimum := worker.lastPlannedRestart.Add(plannedRestartLimit)
		if minimum.After(readyAt) {
			readyAt = minimum
		}
	}
	if now.Before(readyAt) {
		return nil
	}
	return worker.deploy(ctx, now, true)
}

func (worker *Worker) appendClaimed(claimed []postgres.CoreAction) error {
	existing := make(map[int64]struct{}, len(worker.pending)+len(claimed))
	for _, action := range worker.pending {
		existing[action.ID] = struct{}{}
	}
	for _, action := range claimed {
		if action.ID <= 0 || action.TelegramID <= 0 || action.Attempts <= 0 ||
			(action.Action != postgres.CoreActionRevoke && action.Action != postgres.CoreActionReconcile) {
			return errors.New("claimed core action is invalid")
		}
		if _, duplicate := existing[action.ID]; duplicate {
			return errors.New("claimed core action is duplicated")
		}
		existing[action.ID] = struct{}{}
	}
	worker.pending = append(worker.pending, claimed...)
	return nil
}

func pendingKinds(actions []postgres.CoreAction) (hasRevoke bool, hasReconcile bool) {
	for _, action := range actions {
		switch action.Action {
		case postgres.CoreActionRevoke:
			hasRevoke = true
		case postgres.CoreActionReconcile:
			hasReconcile = true
		}
	}
	return hasRevoke, hasReconcile
}

func (worker *Worker) deploy(ctx context.Context, now time.Time, includesPlanned bool) error {
	configuration, err := worker.snapshot.Build(ctx)
	if err != nil {
		return worker.fail(ctx, now, postgres.CoreFailureSnapshot, err)
	}
	if err := worker.installer.Install(ctx, configuration); err != nil {
		return worker.fail(ctx, now, installFailureCode(err), err)
	}
	worker.installedAt = now
	worker.installedPlanned = includesPlanned
	return worker.completeInstalled(ctx, now)
}

func (worker *Worker) completeInstalled(ctx context.Context, now time.Time) error {
	ids := pendingIDs(worker.pending)
	if err := worker.store.Complete(ctx, ids, now); err != nil {
		return fmt.Errorf("complete core actions: %w", err)
	}
	if worker.installedPlanned {
		worker.lastPlannedRestart = worker.installedAt
	}
	worker.pending = nil
	worker.noticeStartedAt = time.Time{}
	worker.installedAt = time.Time{}
	worker.installedPlanned = false
	return nil
}

func (worker *Worker) fail(ctx context.Context, now time.Time, failure postgres.CoreFailureCode, cause error) error {
	errorsToJoin := []error{cause}
	for _, action := range worker.pending {
		if err := worker.store.Retry(ctx, []int64{action.ID}, now.Add(retryDelay(action.Attempts)), failure); err != nil {
			errorsToJoin = append(errorsToJoin, err)
		}
	}
	if err := worker.notifier.NotifyCoreFailure(context.WithoutCancel(ctx), failure); err != nil {
		errorsToJoin = append(errorsToJoin, err)
	}
	worker.pending = nil
	worker.noticeStartedAt = time.Time{}
	return errors.Join(errorsToJoin...)
}

func pendingIDs(actions []postgres.CoreAction) []int64 {
	ids := make([]int64, len(actions))
	for index, action := range actions {
		ids[index] = action.ID
	}
	return ids
}

func retryDelay(attempts int) time.Duration {
	switch attempts {
	case 1:
		return 5 * time.Second
	case 2:
		return 15 * time.Second
	case 3:
		return time.Minute
	case 4:
		return 5 * time.Minute
	default:
		return 15 * time.Minute
	}
}

func installFailureCode(err error) postgres.CoreFailureCode {
	switch singbox.InstallFailureStage(err) {
	case singbox.InstallStageStage, singbox.InstallStageCheck:
		return postgres.CoreFailureCheck
	case singbox.InstallStagePromote, singbox.InstallStageFinalize:
		return postgres.CoreFailurePromote
	case singbox.InstallStageRestart, singbox.InstallStageRollback:
		return postgres.CoreFailureRestart
	default:
		return postgres.CoreFailureRestart
	}
}
