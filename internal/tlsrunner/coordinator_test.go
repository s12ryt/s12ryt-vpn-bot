package tlsrunner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/acme"
)

func TestCoordinatorObtainsWhenUnissuedAndRecordsIssuance(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	expires := now.Add(90 * 24 * time.Hour)
	loader := &loaderStub{settings: duckdnsSettings(), expiresAt: time.Time{}}
	obtainer := &obtainerStub{result: acme.Result{CADirectoryURL: "https://ca.example/directory", NotAfter: expires}}
	recorder := &issuanceRecorderStub{}
	coordinator, err := NewCoordinator(loader, obtainer, recorder, &failureRecorderStub{}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}

	if err := coordinator.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if len(obtainer.calls) != 1 || obtainer.calls[0].Domain != "node.duckdns.org" {
		t.Fatalf("obtainer calls = %#v", obtainer.calls)
	}
	if recorder.calls != 1 || recorder.caURL != "https://ca.example/directory" || !recorder.expiresAt.Equal(expires) || !recorder.now.Equal(now) {
		t.Fatalf("issuance recorder = %+v", recorder)
	}
}

func TestCoordinatorSkipsWhileCertificateIsValid(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	loader := &loaderStub{settings: duckdnsSettings(), expiresAt: now.Add(60 * 24 * time.Hour)}
	obtainer := &obtainerStub{}
	coordinator, _ := NewCoordinator(loader, obtainer, &issuanceRecorderStub{}, &failureRecorderStub{}, func() time.Time { return now })

	if err := coordinator.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if len(obtainer.calls) != 0 {
		t.Fatalf("valid certificate must not trigger issuance: %#v", obtainer.calls)
	}
}

func TestCoordinatorRenewsNearExpiryAndRecordsFailureWhenAllCAsFail(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	loader := &loaderStub{settings: duckdnsSettings(), expiresAt: now.Add(10 * 24 * time.Hour)}
	obtainer := &obtainerStub{err: errors.New("all directories rejected the account")}
	failures := &failureRecorderStub{}
	coordinator, _ := NewCoordinator(loader, obtainer, &issuanceRecorderStub{}, failures, func() time.Time { return now })

	if err := coordinator.Ensure(context.Background()); err == nil {
		t.Fatal("expected issuance error to propagate")
	}
	if len(obtainer.calls) != 1 || failures.calls != 1 || failures.reason != "all_cas_failed" || !failures.now.Equal(now) {
		t.Fatalf("obtainer=%#v failures=%+v", obtainer.calls, failures)
	}
}

func TestCoordinatorSkipsWhenNotConfiguredAndPropagatesLoaderErrors(t *testing.T) {
	now := time.Now().UTC()
	notConfigured := &loaderStub{err: acme.ErrNotConfigured}
	coordinator, _ := NewCoordinator(notConfigured, &obtainerStub{}, &issuanceRecorderStub{}, &failureRecorderStub{}, func() time.Time { return now })
	if err := coordinator.Ensure(context.Background()); err != nil {
		t.Fatalf("unconfigured settings must be skipped, got %v", err)
	}

	broken := &loaderStub{err: errors.New("database unavailable")}
	coordinator, _ = NewCoordinator(broken, &obtainerStub{}, &issuanceRecorderStub{}, &failureRecorderStub{}, func() time.Time { return now })
	if err := coordinator.Ensure(context.Background()); err == nil {
		t.Fatal("loader errors must propagate")
	}
}

func duckdnsSettings() acme.Settings {
	return acme.Settings{
		Mode: acme.ModeDuckDNS, Domain: "node.duckdns.org", Challenge: acme.ChallengeDNS01,
		Email: "owner@example.com", CADirectoryURLs: []string{"https://ca.example/directory"},
		TermsAccepted: true, DNSProviderName: "duckdns", DNSProviderToken: "token",
	}
}

type loaderStub struct {
	settings  acme.Settings
	expiresAt time.Time
	err       error
}

func (stub *loaderStub) LoadForIssuance(context.Context) (acme.Settings, time.Time, error) {
	return stub.settings, stub.expiresAt, stub.err
}

type obtainerStub struct {
	result acme.Result
	err    error
	calls  []acme.Settings
}

func (stub *obtainerStub) Obtain(_ context.Context, settings acme.Settings) (acme.Result, error) {
	stub.calls = append(stub.calls, settings)
	return stub.result, stub.err
}

type issuanceRecorderStub struct {
	calls     int
	caURL     string
	expiresAt time.Time
	now       time.Time
}

func (stub *issuanceRecorderStub) RecordIssuance(_ context.Context, caURL string, expiresAt, now time.Time) error {
	stub.calls++
	stub.caURL, stub.expiresAt, stub.now = caURL, expiresAt, now
	return nil
}

type failureRecorderStub struct {
	calls  int
	reason string
	now    time.Time
}

func (stub *failureRecorderStub) RecordFailure(_ context.Context, reason string, now time.Time) error {
	stub.calls++
	stub.reason, stub.now = reason, now
	return nil
}
