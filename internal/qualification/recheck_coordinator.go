package qualification

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type RecheckSettingsProvider interface {
	RecheckSettings(ctx context.Context) (RecheckSettings, error)
}

type RecheckCoordinator struct {
	settings RecheckSettingsProvider
	users    KnownUserProvider
	rules    RuleProvider
	members  MemberLookup
	writer   DecisionWriter
	notifier RecheckNotifier
	wait     RetryWaitFunc
	now      func() time.Time
	jitter   JitterFunc
}

func NewRecheckCoordinator(
	settings RecheckSettingsProvider,
	users KnownUserProvider,
	rules RuleProvider,
	members MemberLookup,
	writer DecisionWriter,
	notifier RecheckNotifier,
	wait RetryWaitFunc,
	now func() time.Time,
	jitter JitterFunc,
) (*RecheckCoordinator, error) {
	if settings == nil || users == nil || rules == nil || members == nil || writer == nil || notifier == nil {
		return nil, errors.New("recheck coordinator dependencies are required")
	}
	return &RecheckCoordinator{
		settings: settings,
		users:    users,
		rules:    rules,
		members:  members,
		writer:   writer,
		notifier: notifier,
		wait:     wait,
		now:      now,
		jitter:   jitter,
	}, nil
}

func (coordinator *RecheckCoordinator) RunOnce(ctx context.Context) (RecheckSummary, time.Duration, error) {
	if coordinator == nil {
		return RecheckSummary{}, 0, errors.New("recheck coordinator is required")
	}
	settings, err := coordinator.settings.RecheckSettings(ctx)
	if err != nil {
		cause := fmt.Errorf("load qualification recheck settings: %w", err)
		return RecheckSummary{}, 0, coordinator.reportFailure(ctx, cause)
	}
	if err := settings.Validate(); err != nil {
		cause := fmt.Errorf("validate qualification recheck settings: %w", err)
		return RecheckSummary{}, 0, coordinator.reportFailure(ctx, cause)
	}
	members, err := NewRetryingMemberLookup(
		coordinator.members,
		settings.RequestsPerSecond,
		coordinator.wait,
		coordinator.now,
		coordinator.jitter,
	)
	if err != nil {
		return RecheckSummary{}, settings.Interval, coordinator.reportFailure(ctx, err)
	}
	rechecker, err := NewRechecker(
		coordinator.users,
		NewChecker(coordinator.rules, members),
		coordinator.writer,
		coordinator.notifier,
		settings.BatchSize,
	)
	if err != nil {
		return RecheckSummary{}, settings.Interval, coordinator.reportFailure(ctx, err)
	}
	summary, err := rechecker.RunOnce(ctx)
	return summary, settings.Interval, err
}

func (coordinator *RecheckCoordinator) reportFailure(ctx context.Context, cause error) error {
	if notifyErr := coordinator.notifier.NotifyFailure(context.WithoutCancel(ctx), cause); notifyErr != nil {
		return errors.Join(cause, fmt.Errorf("notify qualification recheck failure: %w", notifyErr))
	}
	return cause
}
