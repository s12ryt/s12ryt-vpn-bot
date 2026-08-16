package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestUserManagementStoreListsUsersWithoutCredentialColumns(t *testing.T) {
	database := &accessTransactionStub{row: &accessRowStub{values: []any{[]byte(`[{"telegram_id":12345,"eligible":true,"status":"active","generation":2,"period_started_at":"2026-08-01T00:00:00Z","last_vpn_activity_at":"2026-08-02T00:00:00Z","used_bytes":10,"limit_bytes":100,"quota_blocked":false}]`)}}}
	store := NewUserManagementStore(&transactionRunnerStub{transaction: database}, database)
	users, err := store.ListUsers(context.Background(), 0, 50)
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != 1 || users[0].TelegramID != 12345 || users[0].UsedBytes != 10 {
		t.Fatalf("users = %#v", users)
	}
	if strings.Contains(strings.ToLower(database.querySQL), "credential_bundles") {
		t.Fatalf("query reads credentials: %q", database.querySQL)
	}
}

func TestUserManagementStoreRejectsInvalidListWindow(t *testing.T) {
	database := &accessTransactionStub{}
	store := NewUserManagementStore(&transactionRunnerStub{transaction: database}, database)
	if _, err := store.ListUsers(context.Background(), -1, 50); err == nil || database.querySQL != "" {
		t.Fatalf("invalid list error=%v query=%q", err, database.querySQL)
	}
}

func TestUserManagementStoreFindsExactUserOverview(t *testing.T) {
	database := &accessTransactionStub{row: &accessRowStub{values: []any{[]byte(`{"telegram_id":12345,"eligible":true,"status":"active","generation":2,"period_started_at":"2026-08-01T00:00:00Z","last_vpn_activity_at":"2026-08-02T00:00:00Z","used_bytes":10,"limit_bytes":100,"quota_blocked":false}`)}}}
	store := NewUserManagementStore(&transactionRunnerStub{transaction: database}, database)

	user, err := store.FindUser(context.Background(), 12345)
	if err != nil {
		t.Fatalf("FindUser() error = %v", err)
	}
	if user.TelegramID != 12345 || user.Generation != 2 || user.UsedBytes != 10 {
		t.Fatalf("user = %#v", user)
	}
	if !strings.Contains(database.querySQL, "WHERE vpn_user.telegram_id = $1") || strings.Contains(strings.ToLower(database.querySQL), "credential_bundles") {
		t.Fatalf("query = %q", database.querySQL)
	}
}

func TestUserManagementStoreNormalizesMissingExactUser(t *testing.T) {
	database := &accessTransactionStub{row: &accessRowStub{err: pgx.ErrNoRows}}
	store := NewUserManagementStore(&transactionRunnerStub{transaction: database}, database)
	if _, err := store.FindUser(context.Background(), 12345); !errors.Is(err, ErrVPNUserNotFound) {
		t.Fatalf("FindUser() error = %v", err)
	}
}
