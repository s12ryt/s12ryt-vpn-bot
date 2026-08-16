package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

func TestUserManagementStoreRevokesAndQueuesCoreActionAtomically(t *testing.T) {
	started := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	transaction := &accessTransactionStub{row: &accessRowStub{values: []any{
		int64(12345), true, string(domain.AccessStatusActive), int64(2),
		sql.NullTime{Time: started, Valid: true}, sql.NullTime{Time: started, Valid: true},
	}}}
	runner := &transactionRunnerStub{transaction: transaction}
	store := NewUserManagementStore(runner)
	if err := store.Revoke(context.Background(), 9001, 12345, domain.RevocationModePermanentBlock, started.Add(time.Hour)); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if !runner.committed || len(transaction.execSQL) != 3 {
		t.Fatalf("committed=%v exec=%d", runner.committed, len(transaction.execSQL))
	}
	if !strings.Contains(transaction.execSQL[1], "core_action_outbox") || !strings.Contains(transaction.execSQL[2], "audit_events") {
		t.Fatalf("queries = %#v", transaction.execSQL)
	}
	if got := transaction.execArgs[0][0]; got != string(domain.AccessStatusPermanentlyBlocked) {
		t.Fatalf("persisted status = %#v", got)
	}
}

func TestUserManagementStoreRejectsPendingApprovalWithoutCoreAction(t *testing.T) {
	started := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	transaction := &accessTransactionStub{row: &accessRowStub{values: []any{
		int64(12345), true, string(domain.AccessStatusPendingApproval), int64(1),
		sql.NullTime{Time: started, Valid: true}, sql.NullTime{Time: started, Valid: true},
	}}}
	store := NewUserManagementStore(&transactionRunnerStub{transaction: transaction})
	if err := store.RejectApproval(context.Background(), 9001, 12345, started.Add(time.Hour)); err != nil {
		t.Fatalf("RejectApproval() error = %v", err)
	}
	if len(transaction.execSQL) != 2 || !strings.Contains(transaction.execSQL[1], "audit_events") {
		t.Fatalf("queries = %#v", transaction.execSQL)
	}
	if got := transaction.execArgs[0][0]; got != string(domain.AccessStatusApprovalRejected) {
		t.Fatalf("persisted status = %#v", got)
	}
}

func TestUserManagementStoreRejectsInvalidInputBeforeTransaction(t *testing.T) {
	runner := &transactionRunnerStub{}
	store := NewUserManagementStore(runner)
	if err := store.Revoke(context.Background(), 0, 12345, domain.RevocationModeSelfService, time.Now()); err == nil || runner.called {
		t.Fatalf("invalid actor error=%v transaction=%v", err, runner.called)
	}
}
