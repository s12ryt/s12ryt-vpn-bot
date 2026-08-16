package domain

import (
	"reflect"
	"testing"
	"time"
)

func TestPreviewInactiveAccountsReturnsOnlyActiveAccountsAtBoundary(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	first := activeAccountForTest(t, 30, start)
	second := activeAccountForTest(t, 10, start.Add(24*time.Hour))
	revoked := activeAccountForTest(t, 20, start)
	if _, err := revoked.Revoke(RevocationModeSelfService); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	got, err := PreviewInactiveAccounts([]*AccessAccount{first, revoked, second}, start.Add(7*24*time.Hour), 7)
	if err != nil {
		t.Fatalf("PreviewInactiveAccounts() error = %v", err)
	}
	want := []int64{30}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PreviewInactiveAccounts() = %v, want %v", got, want)
	}
}

func TestPreviewInactiveAccountsSortsTelegramIDsAndSupportsDisabledThreshold(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	accounts := []*AccessAccount{
		activeAccountForTest(t, 30, start),
		activeAccountForTest(t, 10, start),
		activeAccountForTest(t, 20, start),
	}

	got, err := PreviewInactiveAccounts(accounts, start.Add(24*time.Hour), 1)
	if err != nil {
		t.Fatalf("PreviewInactiveAccounts() error = %v", err)
	}
	if want := []int64{10, 20, 30}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PreviewInactiveAccounts() = %v, want %v", got, want)
	}

	disabled, err := PreviewInactiveAccounts(accounts, start.Add(24*time.Hour), 0)
	if err != nil {
		t.Fatalf("PreviewInactiveAccounts() disabled error = %v", err)
	}
	if len(disabled) != 0 {
		t.Fatalf("PreviewInactiveAccounts() disabled = %v, want empty", disabled)
	}
}

func TestPreviewInactiveAccountsRejectsInvalidInput(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	account := activeAccountForTest(t, 10, start)

	if _, err := PreviewInactiveAccounts([]*AccessAccount{account}, start, -1); err == nil {
		t.Fatal("PreviewInactiveAccounts() error = nil, want negative threshold error")
	}
	if _, err := PreviewInactiveAccounts([]*AccessAccount{nil}, start, 1); err == nil {
		t.Fatal("PreviewInactiveAccounts() error = nil, want nil account error")
	}
}

func activeAccountForTest(t *testing.T, telegramID int64, issuedAt time.Time) *AccessAccount {
	t.Helper()
	account, err := NewAccessAccount(telegramID)
	if err != nil {
		t.Fatalf("NewAccessAccount() error = %v", err)
	}
	account.SetEligibility(true)
	if _, err := account.Claim(issuedAt); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	return account
}
