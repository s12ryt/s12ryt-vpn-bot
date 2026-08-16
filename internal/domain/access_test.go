package domain

import (
	"errors"
	"testing"
	"time"
)

func TestAccessAccountRequiresEligibilityForFirstClaim(t *testing.T) {
	account, err := NewAccessAccount(123456789)
	if err != nil {
		t.Fatalf("NewAccessAccount() error = %v", err)
	}

	if _, err := account.Claim(time.Now()); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("Claim() error = %v, want ErrNotEligible", err)
	}
}

func TestAccessAccountRequiresApprovalAfterLeavingAndRejoining(t *testing.T) {
	firstIssuedAt := time.Date(2026, time.August, 16, 8, 0, 0, 0, time.UTC)
	reapprovedAt := firstIssuedAt.Add(48 * time.Hour)
	account, err := NewAccessAccount(123456789)
	if err != nil {
		t.Fatalf("NewAccessAccount() error = %v", err)
	}
	account.SetEligibility(true)

	first, err := account.Claim(firstIssuedAt)
	if err != nil {
		t.Fatalf("Claim(first) error = %v", err)
	}
	if first.CredentialGeneration != 1 || !first.PeriodStartedAt.Equal(firstIssuedAt) {
		t.Fatalf("first issuance = %+v, want generation 1 with a new period", first)
	}
	if account.Snapshot().Status != AccessStatusActive {
		t.Fatalf("status after first claim = %s, want active", account.Snapshot().Status)
	}

	change := account.SetEligibility(false)
	if !change.RevokeCredentialsImmediately {
		fatalf := "leaving an eligibility chat did not request immediate credential revocation"
		t.Fatal(fatalf)
	}
	if account.Snapshot().Status != AccessStatusPendingApproval {
		t.Fatalf("status after leaving = %s, want pending approval", account.Snapshot().Status)
	}

	account.SetEligibility(true)
	if _, err := account.Claim(reapprovedAt); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("Claim(after rejoin) error = %v, want ErrApprovalRequired", err)
	}

	second, err := account.Approve(reapprovedAt)
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if second.CredentialGeneration != 2 {
		t.Fatalf("reapproved credential generation = %d, want 2", second.CredentialGeneration)
	}
	if !second.PeriodStartedAt.Equal(reapprovedAt) {
		t.Fatalf("reapproved period start = %s, want %s", second.PeriodStartedAt, reapprovedAt)
	}
}

func TestAccessAccountRejectsInvalidIdentityAndTimestamps(t *testing.T) {
	if _, err := NewAccessAccount(0); err == nil {
		t.Fatal("NewAccessAccount() accepted a non-positive Telegram ID")
	}

	account, err := NewAccessAccount(1)
	if err != nil {
		t.Fatalf("NewAccessAccount() error = %v", err)
	}
	account.SetEligibility(true)
	if _, err := account.Claim(time.Time{}); err == nil {
		t.Fatal("Claim() accepted a zero issuance timestamp")
	}
}

func TestManualRevocationControlsTheNextClaim(t *testing.T) {
	tests := []struct {
		name       string
		mode       RevocationMode
		wantStatus AccessStatus
		wantErr    error
	}{
		{
			name:       "self service reclaim",
			mode:       RevocationModeSelfService,
			wantStatus: AccessStatusSelfService,
		},
		{
			name:       "administrator review",
			mode:       RevocationModeRequireApproval,
			wantStatus: AccessStatusPendingApproval,
			wantErr:    ErrApprovalRequired,
		},
		{
			name:       "permanent block",
			mode:       RevocationModePermanentBlock,
			wantStatus: AccessStatusPermanentlyBlocked,
			wantErr:    ErrPermanentlyBlocked,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			issuedAt := time.Date(2026, time.August, 16, 8, 0, 0, 0, time.UTC)
			account := newActiveAccount(t, issuedAt)

			change, err := account.Revoke(test.mode)
			if err != nil {
				t.Fatalf("Revoke() error = %v", err)
			}
			if !change.RevokeCredentialsImmediately {
				t.Fatal("manual revocation did not request immediate credential removal")
			}
			if got := account.Snapshot().Status; got != test.wantStatus {
				t.Fatalf("status = %s, want %s", got, test.wantStatus)
			}

			issuance, err := account.Claim(issuedAt.Add(time.Hour))
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Claim() error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr == nil && issuance.CredentialGeneration != 2 {
				t.Fatalf("self-service generation = %d, want 2", issuance.CredentialGeneration)
			}
		})
	}
}

func TestCredentialRotationCanPreserveOrResetTheQuotaPeriod(t *testing.T) {
	issuedAt := time.Date(2026, time.August, 16, 8, 0, 0, 0, time.UTC)
	account := newActiveAccount(t, issuedAt)

	preserved, err := account.Rotate(issuedAt.Add(time.Hour), false)
	if err != nil {
		t.Fatalf("Rotate(preserve) error = %v", err)
	}
	if preserved.CredentialGeneration != 2 || !preserved.PeriodStartedAt.Equal(issuedAt) {
		t.Fatalf("preserved rotation = %+v, want generation 2 and original period", preserved)
	}

	resetAt := issuedAt.Add(2 * time.Hour)
	reset, err := account.Rotate(resetAt, true)
	if err != nil {
		t.Fatalf("Rotate(reset) error = %v", err)
	}
	if reset.CredentialGeneration != 3 || !reset.PeriodStartedAt.Equal(resetAt) {
		t.Fatalf("reset rotation = %+v, want generation 3 and reset period", reset)
	}
	if got := account.Snapshot().LastVPNActivityAt; !got.Equal(resetAt) {
		t.Fatalf("reset rotation activity = %v, want reset time %v", got, resetAt)
	}
}

func TestInactiveAccessIsRemovedAndCanBeReclaimedWhenStillEligible(t *testing.T) {
	issuedAt := time.Date(2026, time.August, 16, 8, 0, 0, 0, time.UTC)
	account := newActiveAccount(t, issuedAt)

	inactive, err := account.IsInactiveAt(issuedAt.Add(24*time.Hour), 1)
	if err != nil {
		t.Fatalf("IsInactiveAt() error = %v", err)
	}
	if !inactive {
		t.Fatal("access was not inactive at the exact one-day boundary")
	}
	change, err := account.RemoveForInactivity()
	if err != nil {
		t.Fatalf("RemoveForInactivity() error = %v", err)
	}
	if !change.RevokeCredentialsImmediately || account.Snapshot().Status != AccessStatusSelfService {
		t.Fatalf("inactivity change = %+v, status = %s", change, account.Snapshot().Status)
	}

	reclaimedAt := issuedAt.Add(25 * time.Hour)
	reclaimed, err := account.Claim(reclaimedAt)
	if err != nil {
		t.Fatalf("Claim(after inactivity) error = %v", err)
	}
	if reclaimed.CredentialGeneration != 2 || !reclaimed.PeriodStartedAt.Equal(reclaimedAt) {
		t.Fatalf("reclaimed issuance = %+v, want fresh credentials and period", reclaimed)
	}
}

func TestVPNActivityMovesTheInactivityBoundary(t *testing.T) {
	issuedAt := time.Date(2026, time.August, 16, 8, 0, 0, 0, time.UTC)
	account := newActiveAccount(t, issuedAt)
	activityAt := issuedAt.Add(12 * time.Hour)

	if err := account.RecordVPNActivity(activityAt, 1); err != nil {
		t.Fatalf("RecordVPNActivity() error = %v", err)
	}
	inactive, err := account.IsInactiveAt(issuedAt.Add(24*time.Hour), 1)
	if err != nil {
		t.Fatalf("IsInactiveAt() error = %v", err)
	}
	if inactive {
		t.Fatal("access became inactive before one day passed since VPN traffic")
	}

	disabled, err := account.IsInactiveAt(issuedAt.Add(365*24*time.Hour), 0)
	if err != nil {
		t.Fatalf("IsInactiveAt(disabled) error = %v", err)
	}
	if disabled {
		t.Fatal("zero-day inactivity policy did not remain disabled")
	}
}

func TestRestoreAccessAccountRoundTripsIssuedSnapshot(t *testing.T) {
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	account := newActiveAccount(t, now)
	snapshot := account.Snapshot()

	restored, err := RestoreAccessAccount(snapshot)
	if err != nil {
		t.Fatalf("RestoreAccessAccount() error = %v", err)
	}
	if got := restored.Snapshot(); got != snapshot {
		t.Fatalf("restored snapshot = %#v, want %#v", got, snapshot)
	}
}

func TestRestoreAccessAccountRejectsInconsistentSnapshots(t *testing.T) {
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	for _, snapshot := range []AccessSnapshot{
		{TelegramID: 0, Status: AccessStatusUnclaimed},
		{TelegramID: 12345, Status: "invalid"},
		{TelegramID: 12345, Status: AccessStatusActive},
		{TelegramID: 12345, Status: AccessStatusUnclaimed, CredentialGeneration: 1, PeriodStartedAt: now, LastVPNActivityAt: now},
		{TelegramID: 12345, Status: AccessStatusActive, CredentialGeneration: 1, PeriodStartedAt: now, LastVPNActivityAt: now.Add(-time.Second)},
	} {
		if _, err := RestoreAccessAccount(snapshot); err == nil {
			t.Fatalf("RestoreAccessAccount(%#v) error = nil", snapshot)
		}
	}
}

func TestRejectedApprovalLeavesNoPendingRequestButCanBeApprovedLater(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	account, err := RestoreAccessAccount(AccessSnapshot{
		TelegramID: 12345, Eligible: true, Status: AccessStatusPendingApproval,
		CredentialGeneration: 1, PeriodStartedAt: now.Add(-time.Hour), LastVPNActivityAt: now.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("RestoreAccessAccount() error = %v", err)
	}
	if err := account.RejectApproval(); err != nil {
		t.Fatalf("RejectApproval() error = %v", err)
	}
	if got := account.Snapshot().Status; got != AccessStatusApprovalRejected {
		t.Fatalf("status = %q, want %q", got, AccessStatusApprovalRejected)
	}
	if _, err := account.Claim(now); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("Claim() error = %v, want ErrApprovalRequired", err)
	}
	issuance, err := account.Approve(now)
	if err != nil || issuance.CredentialGeneration != 2 {
		t.Fatalf("Approve() = %#v, %v", issuance, err)
	}
}

func newActiveAccount(t *testing.T, issuedAt time.Time) *AccessAccount {
	t.Helper()
	account, err := NewAccessAccount(123456789)
	if err != nil {
		t.Fatalf("NewAccessAccount() error = %v", err)
	}
	account.SetEligibility(true)
	if _, err := account.Claim(issuedAt); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	return account
}
