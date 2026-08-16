package postgres

import (
	"context"
	"strings"
	"testing"
)

func TestUserManagementStoreReturnsGlobalStatistics(t *testing.T) {
	database := &accessTransactionStub{row: &accessRowStub{values: []any{int64(10), int64(7), int64(2), int64(1), int64(80_000_000_000)}}}
	store := NewUserManagementStore(&transactionRunnerStub{transaction: database}, database)
	statistics, err := store.Statistics(context.Background())
	if err != nil {
		t.Fatalf("Statistics() error = %v", err)
	}
	if statistics.Total != 10 || statistics.Active != 7 || statistics.Pending != 2 || statistics.Blocked != 1 || statistics.TotalUsedBytes != 80_000_000_000 {
		t.Fatalf("statistics = %#v", statistics)
	}
	if !strings.Contains(database.querySQL, "COUNT(*)") || strings.Contains(strings.ToLower(database.querySQL), "credential_bundles") {
		t.Fatalf("query = %q", database.querySQL)
	}
}
