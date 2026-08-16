package vpn

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

func TestStatusServiceReturnsSharedQuotaAndPrivateLinkForActiveUser(t *testing.T) {
	periodStart := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	users := &statusUserReaderStub{overview: domain.UserOverview{
		TelegramID: 12345, Eligible: true, Status: domain.AccessStatusActive,
		Generation: 2, PeriodStartedAt: periodStart, UsedBytes: 25_000_000_000,
		LimitBytes: 50_000_000_000,
	}}
	credentials := &statusCredentialReaderStub{bundle: domain.CredentialBundle{SubscriptionToken: testStatusToken}}
	links := &statusLinkBuilderStub{url: "https://vpn.example.com/sub/private"}
	service := NewStatusService(users, credentials, links, 30*24*time.Hour)

	status, err := service.GetStatus(context.Background(), 12345)
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}
	if status.Overview.TelegramID != 12345 || status.SubscriptionURL != links.url {
		t.Fatalf("status = %#v", status)
	}
	if want := periodStart.Add(30 * 24 * time.Hour); !status.ResetsAt.Equal(want) {
		t.Fatalf("ResetsAt = %v, want %v", status.ResetsAt, want)
	}
}

func TestStatusServiceDoesNotReadCredentialsForInactiveUser(t *testing.T) {
	users := &statusUserReaderStub{overview: domain.UserOverview{TelegramID: 12345, Status: domain.AccessStatusPendingApproval}}
	credentials := &statusCredentialReaderStub{}
	service := NewStatusService(users, credentials, &statusLinkBuilderStub{}, 30*24*time.Hour)

	status, err := service.GetStatus(context.Background(), 12345)
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}
	if status.SubscriptionURL != "" || credentials.calls != 0 {
		t.Fatalf("status = %#v, credential calls = %d", status, credentials.calls)
	}
}

func TestStatusServiceRejectsMissingUserWithoutLeakingPartialStatus(t *testing.T) {
	service := NewStatusService(&statusUserReaderStub{err: errors.New("database secret")}, &statusCredentialReaderStub{}, &statusLinkBuilderStub{}, 30*24*time.Hour)
	status, err := service.GetStatus(context.Background(), 12345)
	if err == nil || status != (Status{}) {
		t.Fatalf("GetStatus() = (%#v, %v), want zero error", status, err)
	}
}

const testStatusToken = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type statusUserReaderStub struct {
	overview domain.UserOverview
	err      error
}

func (stub *statusUserReaderStub) FindUser(_ context.Context, _ int64) (domain.UserOverview, error) {
	return stub.overview, stub.err
}

type statusCredentialReaderStub struct {
	bundle domain.CredentialBundle
	err    error
	calls  int
}

func (stub *statusCredentialReaderStub) FindActiveByTelegramID(_ context.Context, _ int64) (domain.CredentialBundle, error) {
	stub.calls++
	return stub.bundle, stub.err
}

type statusLinkBuilderStub struct {
	url string
	err error
}

func (stub *statusLinkBuilderStub) SubscriptionURL(_ string) (string, error) {
	return stub.url, stub.err
}
