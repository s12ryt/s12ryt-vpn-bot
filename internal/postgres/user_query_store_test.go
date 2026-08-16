package postgres

import (
	"context"
	"strings"
	"testing"
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
