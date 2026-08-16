package domain

import (
	"errors"
	"testing"
	"time"
)

func TestProvisioningClaimDoesNotMutateAccountWhenCredentialIssueFails(t *testing.T) {
	account, err := NewAccessAccount(12345)
	if err != nil {
		t.Fatalf("NewAccessAccount() error = %v", err)
	}
	account.SetEligibility(true)
	before := account.Snapshot()
	service := NewProvisioningService(stubBundleIssuer{err: errors.New("entropy unavailable")})

	if _, err := service.Claim(account, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("Claim() error = nil, want credential issue error")
	}
	if after := account.Snapshot(); after != before {
		t.Fatalf("Claim() mutated account from %#v to %#v", before, after)
	}
}

func TestProvisioningClaimReturnsCredentialsMatchingIssuance(t *testing.T) {
	account, err := NewAccessAccount(12345)
	if err != nil {
		t.Fatalf("NewAccessAccount() error = %v", err)
	}
	account.SetEligibility(true)
	wantBundle := CredentialBundle{SubscriptionToken: "subscription", VLESSUUID: "vless"}
	service := NewProvisioningService(stubBundleIssuer{bundle: wantBundle})
	issuedAt := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

	grant, err := service.Claim(account, issuedAt)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if grant.Credentials != wantBundle {
		t.Fatalf("Claim() credentials = %#v, want %#v", grant.Credentials, wantBundle)
	}
	if grant.Issuance.CredentialGeneration != 1 || !grant.Issuance.PeriodStartedAt.Equal(issuedAt) {
		t.Fatalf("Claim() issuance = %#v", grant.Issuance)
	}
}

func TestProvisioningRotationReplacesBundleAndPreservesPeriodWhenRequested(t *testing.T) {
	account, err := NewAccessAccount(12345)
	if err != nil {
		t.Fatalf("NewAccessAccount() error = %v", err)
	}
	account.SetEligibility(true)
	periodStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if _, err := account.Claim(periodStart); err != nil {
		t.Fatalf("account Claim() error = %v", err)
	}
	wantBundle := CredentialBundle{SubscriptionToken: "rotated", AnyTLSPassword: "new-password"}
	service := NewProvisioningService(stubBundleIssuer{bundle: wantBundle})

	grant, err := service.Rotate(account, periodStart.Add(24*time.Hour), false)
	if err != nil {
		t.Fatalf("Rotate() error = %v", err)
	}
	if grant.Credentials != wantBundle {
		t.Fatalf("Rotate() credentials = %#v, want %#v", grant.Credentials, wantBundle)
	}
	if grant.Issuance.CredentialGeneration != 2 || !grant.Issuance.PeriodStartedAt.Equal(periodStart) {
		t.Fatalf("Rotate() issuance = %#v, want generation 2 with preserved period", grant.Issuance)
	}
}

func TestProvisioningApproveIssuesFreshCredentialsAndPeriod(t *testing.T) {
	account, err := NewAccessAccount(12345)
	if err != nil {
		t.Fatalf("NewAccessAccount() error = %v", err)
	}
	account.SetEligibility(true)
	firstIssuedAt := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if _, err := account.Claim(firstIssuedAt); err != nil {
		t.Fatalf("account Claim() error = %v", err)
	}
	account.SetEligibility(false)
	account.SetEligibility(true)
	wantBundle := CredentialBundle{SubscriptionToken: "approved", TUICPassword: "new-password"}
	service := NewProvisioningService(stubBundleIssuer{bundle: wantBundle})
	approvedAt := firstIssuedAt.Add(48 * time.Hour)

	grant, err := service.Approve(account, approvedAt)
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if grant.Credentials != wantBundle {
		t.Fatalf("Approve() credentials = %#v, want %#v", grant.Credentials, wantBundle)
	}
	if grant.Issuance.CredentialGeneration != 2 || !grant.Issuance.PeriodStartedAt.Equal(approvedAt) {
		t.Fatalf("Approve() issuance = %#v, want generation 2 with new period", grant.Issuance)
	}
}

type stubBundleIssuer struct {
	bundle CredentialBundle
	err    error
}

func (issuer stubBundleIssuer) Issue() (CredentialBundle, error) {
	return issuer.bundle, issuer.err
}
