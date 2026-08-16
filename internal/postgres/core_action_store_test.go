package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestCoreActionStoreClaimsDueActionsWithAtomicLease(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	database := &coreActionDatabaseStub{row: &coreActionRowStub{
		ids:         []int64{11, 12},
		telegramIDs: []int64{1001, 2002},
		actions:     []string{"revoke", "reconcile"},
		attempts:    []int{1, 3},
	}}
	store := NewCoreActionStore(database)

	actions, err := store.ClaimDue(context.Background(), now, 30*time.Second, 200)
	if err != nil {
		t.Fatalf("ClaimDue() error = %v", err)
	}
	if len(actions) != 2 || actions[0] != (CoreAction{ID: 11, TelegramID: 1001, Action: CoreActionRevoke, Attempts: 1}) ||
		actions[1] != (CoreAction{ID: 12, TelegramID: 2002, Action: CoreActionReconcile, Attempts: 3}) {
		t.Fatalf("ClaimDue() = %#v", actions)
	}
	for _, fragment := range []string{"FOR UPDATE SKIP LOCKED", "available_at <= $1", "attempts + 1", "available_at = $2"} {
		if !strings.Contains(database.query, fragment) {
			t.Fatalf("claim query lacks %q: %s", fragment, database.query)
		}
	}
	if len(database.arguments) != 3 || database.arguments[0] != now || database.arguments[1] != now.Add(30*time.Second) || database.arguments[2] != 200 {
		t.Fatalf("claim arguments = %#v", database.arguments)
	}
}

func TestCoreActionStoreRejectsInvalidClaimedRowsWithoutPartialResults(t *testing.T) {
	tests := []struct {
		name string
		row  *coreActionRowStub
	}{
		{name: "mismatched arrays", row: &coreActionRowStub{ids: []int64{1}, telegramIDs: nil, actions: []string{"revoke"}, attempts: []int{1}}},
		{name: "unknown action", row: &coreActionRowStub{ids: []int64{1}, telegramIDs: []int64{1001}, actions: []string{"reload"}, attempts: []int{1}}},
		{name: "invalid identity", row: &coreActionRowStub{ids: []int64{0}, telegramIDs: []int64{1001}, actions: []string{"revoke"}, attempts: []int{1}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := NewCoreActionStore(&coreActionDatabaseStub{row: test.row})
			actions, err := store.ClaimDue(context.Background(), time.Now(), time.Second, 10)
			if err == nil || actions != nil {
				t.Fatalf("ClaimDue() = %#v, %v", actions, err)
			}
		})
	}
}

func TestCoreActionStoreCompletesAndRetriesOnlyValidatedIDsAndReasonCodes(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	database := &coreActionDatabaseStub{}
	store := NewCoreActionStore(database)
	if err := store.Complete(context.Background(), []int64{11, 12}, now); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if !strings.Contains(database.execQueries[0], "completed_at") || len(database.execArguments[0]) != 2 {
		t.Fatalf("Complete() query=%q args=%#v", database.execQueries[0], database.execArguments[0])
	}
	if err := store.Retry(context.Background(), []int64{13}, now.Add(time.Minute), CoreFailureCheck); err != nil {
		t.Fatalf("Retry() error = %v", err)
	}
	if !strings.Contains(database.execQueries[1], "last_error") || database.execArguments[1][2] != string(CoreFailureCheck) {
		t.Fatalf("Retry() query=%q args=%#v", database.execQueries[1], database.execArguments[1])
	}

	before := len(database.execQueries)
	if err := store.Complete(context.Background(), []int64{11, 11}, now); err == nil {
		t.Fatal("Complete() accepted duplicate IDs")
	}
	if err := store.Retry(context.Background(), []int64{13}, now, CoreFailureCode("secret=value")); err == nil {
		t.Fatal("Retry() accepted arbitrary failure text")
	}
	if len(database.execQueries) != before {
		t.Fatal("invalid completion or retry reached database")
	}
}

type coreActionDatabaseStub struct {
	query         string
	arguments     []any
	row           pgx.Row
	execQueries   []string
	execArguments [][]any
}

func (stub *coreActionDatabaseStub) QueryRow(_ context.Context, query string, arguments ...any) pgx.Row {
	stub.query = query
	stub.arguments = arguments
	return stub.row
}

func (stub *coreActionDatabaseStub) Exec(_ context.Context, query string, arguments ...any) (pgconn.CommandTag, error) {
	stub.execQueries = append(stub.execQueries, query)
	stub.execArguments = append(stub.execArguments, arguments)
	return pgconn.CommandTag{}, nil
}

type coreActionRowStub struct {
	ids         []int64
	telegramIDs []int64
	actions     []string
	attempts    []int
	err         error
}

func (row *coreActionRowStub) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != 4 {
		return errors.New("unexpected core action destination count")
	}
	*destinations[0].(*[]int64) = row.ids
	*destinations[1].(*[]int64) = row.telegramIDs
	*destinations[2].(*[]string) = row.actions
	*destinations[3].(*[]int) = row.attempts
	return nil
}
