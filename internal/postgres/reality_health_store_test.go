package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/reality"
)

func TestRealityHealthStoreReadsConfiguredPublicTarget(t *testing.T) {
	database := &realityHealthDatabaseStub{row: &trafficRowStub{values: []any{"www.example.com"}}}
	store := NewRealityHealthStore(nil, database)
	target, err := store.CurrentRealityTarget(context.Background())
	if err != nil {
		t.Fatalf("CurrentRealityTarget() error = %v", err)
	}
	if target != "www.example.com" {
		t.Fatalf("target = %q", target)
	}
	if strings.Contains(database.querySQL, "private_key") || !strings.Contains(database.querySQL, "configured = TRUE") {
		t.Fatalf("target query = %q", database.querySQL)
	}
}

func TestRealityHealthStoreNormalizesMissingTarget(t *testing.T) {
	database := &realityHealthDatabaseStub{row: &trafficRowStub{err: pgx.ErrNoRows}}
	_, err := NewRealityHealthStore(nil, database).CurrentRealityTarget(context.Background())
	if !errors.Is(err, reality.ErrRealityTargetNotConfigured) {
		t.Fatalf("CurrentRealityTarget() error = %v", err)
	}
}

type realityHealthDatabaseStub struct {
	row      pgx.Row
	querySQL string
}

func (stub *realityHealthDatabaseStub) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	stub.querySQL = query
	return stub.row
}

func (*realityHealthDatabaseStub) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func TestRealityHealthStorePersistsFailureAndRecoveryTransitions(t *testing.T) {
	now := time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name       string
		row        pgx.Row
		healthy    bool
		transition reality.HealthTransition
	}{
		{name: "initial failure", row: &trafficRowStub{err: pgx.ErrNoRows}, healthy: false, transition: reality.HealthTransitionFailed},
		{name: "continued acknowledged failure", row: &trafficRowStub{values: []any{"www.example.com", false, ""}}, healthy: false, transition: reality.HealthTransitionNone},
		{name: "continued pending failure", row: &trafficRowStub{values: []any{"www.example.com", false, "failed"}}, healthy: false, transition: reality.HealthTransitionFailed},
		{name: "recovery", row: &trafficRowStub{values: []any{"www.example.com", false, ""}}, healthy: true, transition: reality.HealthTransitionRecovered},
		{name: "new healthy target", row: &trafficRowStub{values: []any{"old.example.com", false, "failed"}}, healthy: true, transition: reality.HealthTransitionNone},
		{name: "new failed target", row: &trafficRowStub{values: []any{"old.example.com", true, ""}}, healthy: false, transition: reality.HealthTransitionFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			transaction := &trafficTransactionStub{rows: []pgx.Row{test.row}}
			runner := &trafficTransactionRunnerStub{transaction: transaction}
			store := NewRealityHealthStore(runner, nil)
			transition, err := store.RecordRealityHealth(context.Background(), "www.example.com", test.healthy, now)
			if err != nil {
				t.Fatalf("RecordRealityHealth() error = %v", err)
			}
			if transition != test.transition || !runner.committed || len(transaction.execSQL) != 1 {
				t.Fatalf("transition=%q committed=%v SQL=%#v", transition, runner.committed, transaction.execSQL)
			}
			if !strings.Contains(transaction.execSQL[0], "reality_health") {
				t.Fatalf("write SQL = %q", transaction.execSQL[0])
			}
		})
	}
}

func TestRealityHealthStoreAcknowledgesOnlyMatchingPendingTransition(t *testing.T) {
	now := time.Date(2026, 8, 18, 10, 1, 0, 0, time.UTC)
	transaction := &trafficTransactionStub{}
	runner := &trafficTransactionRunnerStub{transaction: transaction}
	store := NewRealityHealthStore(runner, nil)
	if err := store.AcknowledgeRealityHealthNotification(context.Background(), "www.example.com", reality.HealthTransitionFailed, now); err != nil {
		t.Fatalf("AcknowledgeRealityHealthNotification() error = %v", err)
	}
	if !runner.committed || len(transaction.execSQL) != 1 ||
		!strings.Contains(transaction.execSQL[0], "notification_pending = NULL") ||
		!strings.Contains(transaction.execSQL[0], "notification_pending = $2") {
		t.Fatalf("committed=%v SQL=%#v", runner.committed, transaction.execSQL)
	}
}

func TestRealityHealthStoreRejectsInvalidObservationBeforeTransaction(t *testing.T) {
	runner := &trafficTransactionRunnerStub{}
	store := NewRealityHealthStore(runner, nil)
	if _, err := store.RecordRealityHealth(context.Background(), "invalid target", false, time.Now()); err == nil || runner.called {
		t.Fatalf("invalid observation error=%v transaction=%v", err, runner.called)
	}
	if _, err := store.RecordRealityHealth(context.Background(), "www.example.com", false, time.Time{}); err == nil || runner.called {
		t.Fatalf("zero time error=%v transaction=%v", err, runner.called)
	}
}
