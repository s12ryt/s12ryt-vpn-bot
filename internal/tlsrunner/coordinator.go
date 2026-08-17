package tlsrunner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/acme"
)

// renewalMargin starts renewal attempts this long before expiry so a failing
// CA still leaves room for retries before the certificate lapses.
const renewalMargin = 30 * 24 * time.Hour

// checkInterval is how often the scheduler re-evaluates certificate state.
const checkInterval = time.Hour

type SettingsLoader interface {
	LoadForIssuance(context.Context) (acme.Settings, time.Time, error)
}

type CertificateObtainer interface {
	Obtain(context.Context, acme.Settings) (acme.Result, error)
}

type IssuanceRecorder interface {
	RecordIssuance(context.Context, string, time.Time, time.Time) error
}

type FailureRecorder interface {
	RecordFailure(context.Context, string, time.Time) error
}

// Coordinator decides when a certificate must be obtained or renewed and
// persists the outcome. Until a certificate is valid, VPN protocols stay
// unissued and no nodes are handed out.
type Coordinator struct {
	settings SettingsLoader
	obtainer CertificateObtainer
	issuance IssuanceRecorder
	failures FailureRecorder
	now      func() time.Time
}

func NewCoordinator(settings SettingsLoader, obtainer CertificateObtainer, issuance IssuanceRecorder, failures FailureRecorder, now func() time.Time) (*Coordinator, error) {
	if settings == nil || obtainer == nil || issuance == nil || failures == nil {
		return nil, errors.New("TLS coordinator dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Coordinator{settings: settings, obtainer: obtainer, issuance: issuance, failures: failures, now: now}, nil
}

func (coordinator *Coordinator) Ensure(ctx context.Context) error {
	if coordinator == nil || coordinator.settings == nil {
		return errors.New("TLS coordinator is not initialized")
	}
	acmeSettings, expiresAt, err := coordinator.settings.LoadForIssuance(ctx)
	if errors.Is(err, acme.ErrNotConfigured) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load TLS settings: %w", err)
	}
	now := coordinator.now().UTC()
	if !expiresAt.IsZero() && now.Before(expiresAt.Add(-renewalMargin)) {
		return nil
	}
	result, obtainErr := coordinator.obtainer.Obtain(ctx, acmeSettings)
	if obtainErr != nil {
		recordErr := coordinator.failures.RecordFailure(context.WithoutCancel(ctx), "all_cas_failed", now)
		return errors.Join(fmt.Errorf("obtain TLS certificate: %w", obtainErr), recordErr)
	}
	if err := coordinator.issuance.RecordIssuance(ctx, result.CADirectoryURL, result.NotAfter, now); err != nil {
		return fmt.Errorf("record TLS issuance: %w", err)
	}
	return nil
}

// Run executes Ensure immediately and then once per hour until the context is
// cancelled; individual failures never stop the loop.
func (coordinator *Coordinator) Run(ctx context.Context, wait func(context.Context, time.Duration) error) error {
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
		if err := coordinator.Ensure(ctx); err != nil && !errors.Is(err, context.Canceled) {
			_ = err // logged by the caller; keep renewing on the next tick
		}
		if err := wait(ctx, checkInterval); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
	}
}
