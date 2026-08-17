package reality

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrRealityTargetNotConfigured = errors.New("REALITY target is not configured")

type HealthTransition string

const (
	HealthTransitionNone      HealthTransition = "none"
	HealthTransitionFailed    HealthTransition = "failed"
	HealthTransitionRecovered HealthTransition = "recovered"
)

type RealityTargetProvider interface {
	CurrentRealityTarget(context.Context) (string, error)
}

type RealityHealthRecorder interface {
	RecordRealityHealth(context.Context, string, bool, time.Time) (HealthTransition, error)
	AcknowledgeRealityHealthNotification(context.Context, string, HealthTransition, time.Time) error
}

type RealityHealthNotifier interface {
	NotifyRealityFailure(context.Context, string) error
	NotifyRealityRecovery(context.Context, string) error
}

type HealthWaitFunc func(context.Context, time.Duration) error

type HealthMonitor struct {
	targets  RealityTargetProvider
	prober   Prober
	recorder RealityHealthRecorder
	notifier RealityHealthNotifier
	now      func() time.Time
}

func NewHealthMonitor(targets RealityTargetProvider, prober Prober, recorder RealityHealthRecorder, notifier RealityHealthNotifier, now func() time.Time) (*HealthMonitor, error) {
	if targets == nil || prober == nil || recorder == nil || notifier == nil || now == nil {
		return nil, errors.New("REALITY health monitor dependencies are required")
	}
	return &HealthMonitor{targets: targets, prober: prober, recorder: recorder, notifier: notifier, now: now}, nil
}

func (monitor *HealthMonitor) Check(ctx context.Context) error {
	if monitor == nil || monitor.targets == nil || monitor.prober == nil || monitor.recorder == nil || monitor.notifier == nil || monitor.now == nil {
		return errors.New("REALITY health monitor is not configured")
	}
	if ctx == nil {
		return errors.New("REALITY health check requires a context")
	}
	target, err := monitor.targets.CurrentRealityTarget(ctx)
	if errors.Is(err, ErrRealityTargetNotConfigured) {
		return nil
	}
	if err != nil {
		return err
	}
	target = strings.TrimSpace(target)
	if err := ValidateTargetDomain(target); err != nil {
		return errors.New("configured REALITY target is invalid")
	}

	probe, probeErr := monitor.prober.Probe(ctx, target)
	healthy := probeErr == nil && probe.Domain == target && probe.TLS13 && probe.Latency > 0
	at := monitor.now().UTC()
	if at.IsZero() {
		return errors.New("REALITY health check time is invalid")
	}
	transition, err := monitor.recorder.RecordRealityHealth(ctx, target, healthy, at)
	if err != nil {
		return err
	}
	switch transition {
	case HealthTransitionNone:
		return nil
	case HealthTransitionFailed:
		if err := monitor.notifier.NotifyRealityFailure(ctx, target); err != nil {
			return err
		}
	case HealthTransitionRecovered:
		if err := monitor.notifier.NotifyRealityRecovery(ctx, target); err != nil {
			return err
		}
	default:
		return errors.New("persisted REALITY health transition is invalid")
	}
	if err := monitor.recorder.AcknowledgeRealityHealthNotification(ctx, target, transition, at); err != nil {
		return err
	}
	return nil
}

func ValidateTargetDomain(target string) error {
	if target != strings.TrimSpace(target) || !validDomain(target) {
		return errors.New("REALITY target domain is invalid")
	}
	return nil
}

func (monitor *HealthMonitor) Run(ctx context.Context, wait HealthWaitFunc) error {
	if monitor == nil {
		return errors.New("REALITY health monitor is not configured")
	}
	if ctx == nil {
		return errors.New("REALITY health monitor requires a context")
	}
	if wait == nil {
		wait = waitForRealityHealth
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		_ = monitor.Check(ctx)
		if err := wait(ctx, time.Hour); err != nil && ctx.Err() != nil {
			return nil
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

func waitForRealityHealth(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
