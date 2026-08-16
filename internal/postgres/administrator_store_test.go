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

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
)

func TestAdministratorStoreSetsRoleWithSerializedOwnerProtectionAndAudit(t *testing.T) {
	now := time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC)
	transaction := &administratorTransactionStub{rows: []pgx.Row{
		&administratorRowStub{values: []any{string(auth.RoleOwner), true, true, sql.NullString{}, sql.NullBool{}, sql.NullBool{}, int64(1)}},
	}}
	runner := &administratorTransactionRunnerStub{transaction: transaction}
	store := NewAdministratorStore(runner, transaction)

	if err := store.SetRole(context.Background(), 9001, 12345, auth.RoleAdministrator, now); err != nil {
		t.Fatalf("SetRole() error = %v", err)
	}
	if !runner.committed || len(transaction.execSQL) != 3 {
		t.Fatalf("committed=%v exec=%d", runner.committed, len(transaction.execSQL))
	}
	if !strings.Contains(transaction.execSQL[0], "pg_advisory_xact_lock") {
		t.Fatalf("first query does not serialize role changes: %q", transaction.execSQL[0])
	}
	if !strings.Contains(transaction.execSQL[1], "INSERT INTO administrators") || !strings.Contains(transaction.execSQL[1], "ON CONFLICT") {
		t.Fatalf("role upsert = %q", transaction.execSQL[1])
	}
	if !strings.Contains(transaction.execSQL[2], "audit_events") || transaction.execArgs[2][0] != int64(9001) {
		t.Fatalf("audit = %q %#v", transaction.execSQL[2], transaction.execArgs[2])
	}
}

func TestAdministratorStoreRemovesAdministratorAndRevokesAuthentication(t *testing.T) {
	now := time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC)
	transaction := &administratorTransactionStub{rows: []pgx.Row{
		&administratorRowStub{values: []any{string(auth.RoleOwner), true, true, sql.NullString{String: string(auth.RoleAdministrator), Valid: true}, sql.NullBool{Bool: false, Valid: true}, sql.NullBool{Bool: true, Valid: true}, int64(2)}},
	}}
	runner := &administratorTransactionRunnerStub{transaction: transaction}
	store := NewAdministratorStore(runner, transaction)

	if err := store.Remove(context.Background(), 9001, 12345, now); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if !runner.committed || len(transaction.execSQL) != 5 {
		t.Fatalf("committed=%v exec=%d", runner.committed, len(transaction.execSQL))
	}
	if !strings.Contains(transaction.execSQL[1], "UPDATE administrators") || !strings.Contains(transaction.execSQL[2], "DELETE FROM admin_login_codes") || !strings.Contains(transaction.execSQL[3], "DELETE FROM admin_sessions") {
		t.Fatalf("remove queries = %#v", transaction.execSQL)
	}
}

func TestAdministratorStoreProtectsRootAndLastOwnerBeforeMutation(t *testing.T) {
	now := time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC)
	for _, testCase := range []struct {
		name       string
		targetRoot bool
		ownerCount int64
		want       error
	}{
		{name: "root", targetRoot: true, ownerCount: 2, want: auth.ErrRootOwnerProtected},
		{name: "last owner", ownerCount: 1, want: auth.ErrLastOwnerProtected},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			transaction := &administratorTransactionStub{rows: []pgx.Row{
				&administratorRowStub{values: []any{string(auth.RoleOwner), true, true, sql.NullString{String: string(auth.RoleOwner), Valid: true}, sql.NullBool{Bool: testCase.targetRoot, Valid: true}, sql.NullBool{Bool: true, Valid: true}, testCase.ownerCount}},
			}}
			runner := &administratorTransactionRunnerStub{transaction: transaction}
			store := NewAdministratorStore(runner, transaction)
			err := store.Remove(context.Background(), 9001, 12345, now)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Remove() error = %v, want %v", err, testCase.want)
			}
			if runner.committed || len(transaction.execSQL) != 1 {
				t.Fatalf("committed=%v queries=%#v", runner.committed, transaction.execSQL)
			}
		})
	}
}

func TestAdministratorStoreRejectsNonOwnerAndInvalidInputBeforeRoleMutation(t *testing.T) {
	now := time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC)
	transaction := &administratorTransactionStub{rows: []pgx.Row{
		&administratorRowStub{values: []any{string(auth.RoleAdministrator), false, true, sql.NullString{}, sql.NullBool{}, sql.NullBool{}, int64(1)}},
	}}
	runner := &administratorTransactionRunnerStub{transaction: transaction}
	store := NewAdministratorStore(runner, transaction)
	if err := store.SetRole(context.Background(), 9001, 12345, auth.RoleAdministrator, now); !errors.Is(err, auth.ErrRoleManagementForbidden) {
		t.Fatalf("SetRole() error = %v", err)
	}
	if runner.committed || len(transaction.execSQL) != 1 {
		t.Fatalf("committed=%v queries=%#v", runner.committed, transaction.execSQL)
	}

	invalidRunner := &administratorTransactionRunnerStub{}
	invalidStore := NewAdministratorStore(invalidRunner, transaction)
	if err := invalidStore.SetRole(context.Background(), 0, 12345, auth.RoleAdministrator, now); err == nil || invalidRunner.called {
		t.Fatalf("invalid input error=%v transaction=%v", err, invalidRunner.called)
	}
}

func TestAdministratorStoreListsActiveAdministratorsInStableOrder(t *testing.T) {
	database := &administratorTransactionStub{rows: []pgx.Row{
		&administratorRowStub{values: []any{[]byte(`[{"telegram_id":101,"role":"owner","root":true,"active":true},{"telegram_id":202,"role":"administrator","root":false,"active":true}]`)}},
	}}
	store := NewAdministratorStore(&administratorTransactionRunnerStub{}, database)
	administrators, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(administrators) != 2 || administrators[0].TelegramID != 101 || administrators[1].TelegramID != 202 {
		t.Fatalf("administrators = %#v", administrators)
	}
	if !strings.Contains(database.querySQL[0], "ORDER BY") || !strings.Contains(database.querySQL[0], "WHERE active") {
		t.Fatalf("query = %q", database.querySQL[0])
	}
}

type administratorTransactionRunnerStub struct {
	transaction *administratorTransactionStub
	called      bool
	committed   bool
}

func (runner *administratorTransactionRunnerStub) RunInTransaction(ctx context.Context, operation func(Database) error) error {
	runner.called = true
	if runner.transaction == nil {
		return errors.New("missing transaction")
	}
	if err := operation(runner.transaction); err != nil {
		return err
	}
	runner.committed = true
	return nil
}

type administratorTransactionStub struct {
	execSQL   []string
	execArgs  [][]any
	querySQL  []string
	queryArgs [][]any
	rows      []pgx.Row
}

func (stub *administratorTransactionStub) Exec(_ context.Context, query string, arguments ...any) (commandTag pgconn.CommandTag, err error) {
	stub.execSQL = append(stub.execSQL, query)
	stub.execArgs = append(stub.execArgs, arguments)
	return pgconn.CommandTag{}, nil
}

func (stub *administratorTransactionStub) QueryRow(_ context.Context, query string, arguments ...any) pgx.Row {
	stub.querySQL = append(stub.querySQL, query)
	stub.queryArgs = append(stub.queryArgs, arguments)
	index := len(stub.querySQL) - 1
	if index >= len(stub.rows) {
		return &administratorRowStub{err: pgx.ErrNoRows}
	}
	return stub.rows[index]
}

type administratorRowStub struct {
	values []any
	err    error
}

func (stub *administratorRowStub) Scan(destinations ...any) error {
	if stub.err != nil {
		return stub.err
	}
	if len(destinations) != len(stub.values) {
		return errors.New("unexpected scan destination count")
	}
	for index, destination := range destinations {
		switch pointer := destination.(type) {
		case *string:
			*pointer = stub.values[index].(string)
		case *bool:
			*pointer = stub.values[index].(bool)
		case *int64:
			*pointer = stub.values[index].(int64)
		case *sql.NullString:
			*pointer = stub.values[index].(sql.NullString)
		case *sql.NullBool:
			*pointer = stub.values[index].(sql.NullBool)
		case *[]byte:
			*pointer = stub.values[index].([]byte)
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}
