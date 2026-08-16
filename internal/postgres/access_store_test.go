package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

func TestAccessStoreAppliesExplicitIneligibilityAtomically(t *testing.T) {
	periodStartedAt := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	lastActivityAt := periodStartedAt.Add(2 * time.Hour)
	transaction := &accessTransactionStub{row: &accessRowStub{values: []any{
		int64(12345), true, string(domain.AccessStatusActive), int64(3),
		sql.NullTime{Time: periodStartedAt, Valid: true},
		sql.NullTime{Time: lastActivityAt, Valid: true},
	}}}
	runner := &transactionRunnerStub{transaction: transaction}
	store := NewAccessStore(runner)

	change, err := store.ApplyQualification(context.Background(), 12345, domain.QualificationIneligible)
	if err != nil {
		t.Fatalf("ApplyQualification() error = %v", err)
	}
	if !change.RevokeCredentialsImmediately {
		t.Fatal("ApplyQualification() did not request immediate credential revocation")
	}
	if !runner.committed || runner.rolledBack {
		t.Fatalf("transaction committed=%v rolledBack=%v", runner.committed, runner.rolledBack)
	}
	if len(transaction.execSQL) != 3 {
		t.Fatalf("Exec calls = %d, want insert, update, and durable revoke action", len(transaction.execSQL))
	}
	if !strings.Contains(transaction.execSQL[0], "INSERT INTO vpn_users") || !strings.Contains(transaction.execSQL[0], "ON CONFLICT") {
		t.Fatalf("first query does not ensure the user: %q", transaction.execSQL[0])
	}
	if !strings.Contains(transaction.querySQL, "FOR UPDATE") {
		t.Fatalf("account query does not lock the user row: %q", transaction.querySQL)
	}
	if !strings.Contains(transaction.execSQL[1], "UPDATE vpn_users") {
		t.Fatalf("second query does not persist the account: %q", transaction.execSQL[1])
	}
	if !strings.Contains(transaction.execSQL[2], "INSERT INTO core_action_outbox") ||
		!strings.Contains(transaction.execSQL[2], "revoke") {
		t.Fatalf("third query does not queue durable revocation: %q", transaction.execSQL[2])
	}
	if len(transaction.execArgs[2]) != 1 || transaction.execArgs[2][0] != int64(12345) {
		t.Fatalf("revoke action args = %#v", transaction.execArgs[2])
	}
	wantArgs := []any{false, string(domain.AccessStatusPendingApproval), int64(3), periodStartedAt, lastActivityAt, int64(12345)}
	if !equalAccessArguments(transaction.execArgs[1], wantArgs) {
		t.Fatalf("update args = %#v, want %#v", transaction.execArgs[1], wantArgs)
	}
}

func TestAccessStoreDoesNotOpenTransactionForIndeterminateQualification(t *testing.T) {
	runner := &transactionRunnerStub{}
	store := NewAccessStore(runner)

	change, err := store.ApplyQualification(context.Background(), 12345, domain.QualificationIndeterminate)
	if err != nil {
		t.Fatalf("ApplyQualification() error = %v", err)
	}
	if change != (domain.AccessChange{}) {
		t.Fatalf("ApplyQualification() change = %#v, want zero", change)
	}
	if runner.called {
		t.Fatal("indeterminate qualification opened a database transaction")
	}
}

func TestAccessStoreDoesNotQueueDuplicateRevocationForAlreadyIneligibleAccount(t *testing.T) {
	periodStartedAt := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	transaction := &accessTransactionStub{row: &accessRowStub{values: []any{
		int64(12345), false, string(domain.AccessStatusPendingApproval), int64(1),
		sql.NullTime{Time: periodStartedAt, Valid: true},
		sql.NullTime{Time: periodStartedAt, Valid: true},
	}}}
	store := NewAccessStore(&transactionRunnerStub{transaction: transaction})

	change, err := store.ApplyQualification(context.Background(), 12345, domain.QualificationIneligible)
	if err != nil {
		t.Fatalf("ApplyQualification() error = %v", err)
	}
	if change.RevokeCredentialsImmediately {
		t.Fatal("already-ineligible account requested duplicate revocation")
	}
	if len(transaction.execSQL) != 2 {
		t.Fatalf("Exec calls = %d, want no outbox action", len(transaction.execSQL))
	}
}

func TestAccessStoreRollsBackWhenPersistingQualificationFails(t *testing.T) {
	wantErr := errors.New("database unavailable")
	transaction := &accessTransactionStub{execErrAt: 1, execErr: wantErr, row: &accessRowStub{values: []any{
		int64(12345), false, string(domain.AccessStatusUnclaimed), int64(0), sql.NullTime{}, sql.NullTime{},
	}}}
	runner := &transactionRunnerStub{transaction: transaction}
	store := NewAccessStore(runner)

	if _, err := store.ApplyQualification(context.Background(), 12345, domain.QualificationEligible); !errors.Is(err, wantErr) {
		t.Fatalf("ApplyQualification() error = %v, want persistence failure", err)
	}
	if runner.committed || !runner.rolledBack {
		t.Fatalf("transaction committed=%v rolledBack=%v", runner.committed, runner.rolledBack)
	}
}

func TestAccessStoreRejectsInvalidInputBeforeTransaction(t *testing.T) {
	for _, testCase := range []struct {
		telegramID int64
		decision   domain.QualificationDecision
	}{
		{telegramID: 0, decision: domain.QualificationEligible},
		{telegramID: 12345, decision: domain.QualificationDecision("unknown")},
	} {
		runner := &transactionRunnerStub{}
		store := NewAccessStore(runner)
		if _, err := store.ApplyQualification(context.Background(), testCase.telegramID, testCase.decision); err == nil {
			t.Fatalf("ApplyQualification(%d, %q) error = nil", testCase.telegramID, testCase.decision)
		}
		if runner.called {
			t.Fatalf("ApplyQualification(%d, %q) opened a transaction", testCase.telegramID, testCase.decision)
		}
	}
}

type transactionRunnerStub struct {
	transaction *accessTransactionStub
	called      bool
	committed   bool
	rolledBack  bool
}

func (runner *transactionRunnerStub) RunInTransaction(ctx context.Context, operation func(Database) error) error {
	runner.called = true
	err := operation(runner.transaction)
	if err != nil {
		runner.rolledBack = true
		return err
	}
	runner.committed = true
	return nil
}

type accessTransactionStub struct {
	execSQL   []string
	execArgs  [][]any
	execErrAt int
	execErr   error
	querySQL  string
	queryArgs []any
	row       pgx.Row
}

func (stub *accessTransactionStub) Exec(_ context.Context, query string, arguments ...any) (pgconn.CommandTag, error) {
	stub.execSQL = append(stub.execSQL, query)
	stub.execArgs = append(stub.execArgs, arguments)
	if stub.execErr != nil && len(stub.execSQL)-1 == stub.execErrAt {
		return pgconn.CommandTag{}, stub.execErr
	}
	return pgconn.CommandTag{}, nil
}

func (stub *accessTransactionStub) QueryRow(_ context.Context, query string, arguments ...any) pgx.Row {
	stub.querySQL = query
	stub.queryArgs = arguments
	return stub.row
}

type accessRowStub struct {
	values []any
	err    error
}

func (stub *accessRowStub) Scan(destinations ...any) error {
	if stub.err != nil {
		return stub.err
	}
	if len(destinations) != len(stub.values) {
		return errors.New("unexpected scan destination count")
	}
	for index, destination := range destinations {
		switch pointer := destination.(type) {
		case *int64:
			*pointer = stub.values[index].(int64)
		case *string:
			*pointer = stub.values[index].(string)
		case *bool:
			*pointer = stub.values[index].(bool)
		case *sql.NullTime:
			*pointer = stub.values[index].(sql.NullTime)
		case *[]byte:
			*pointer = stub.values[index].([]byte)
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}

func equalAccessArguments(got, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
