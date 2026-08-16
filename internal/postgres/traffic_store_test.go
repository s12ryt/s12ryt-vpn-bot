package postgres

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/trafficstats"
)

func TestTrafficStoreRecordsBatchAndQueuesQuotaRevocationAtomically(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	observedAt := start.Add(time.Minute)
	transaction := &trafficTransactionStub{rows: []pgx.Row{&trafficRowStub{values: []any{
		int64(1001), true, "active", int64(1),
		sql.NullTime{Time: start, Valid: true}, sql.NullTime{Time: start, Valid: true},
		start, int64(2592000), int64(100), int64(90), false,
	}}}}
	runner := &trafficTransactionRunnerStub{transaction: transaction}
	store := NewTrafficStore(runner)

	result, err := store.RecordBatch(context.Background(), observedAt, []trafficstats.Sample{
		{TelegramID: 1001, Uplink: 4, Downlink: 6},
	})
	if err != nil {
		t.Fatalf("RecordBatch() error = %v", err)
	}
	if result.Applied != 1 || len(result.RevokedTelegramIDs) != 1 || result.RevokedTelegramIDs[0] != 1001 || len(result.RestoredTelegramIDs) != 0 {
		t.Fatalf("RecordBatch() = %#v", result)
	}
	if !runner.committed || runner.rolledBack {
		t.Fatalf("transaction committed=%v rolledBack=%v", runner.committed, runner.rolledBack)
	}
	if len(transaction.execSQL) != 3 ||
		!strings.Contains(transaction.execSQL[0], "UPDATE vpn_users") ||
		!strings.Contains(transaction.execSQL[1], "UPDATE quota_windows") ||
		!strings.Contains(transaction.execSQL[2], "core_action_outbox") ||
		len(transaction.execArgs[2]) != 2 || transaction.execArgs[2][1] != "revoke" {
		t.Fatalf("transaction writes = %#v", transaction.execSQL)
	}
	quotaArgs := transaction.execArgs[1]
	if quotaArgs[1] != int64(100) || quotaArgs[2] != true || quotaArgs[3] != int64(1001) {
		t.Fatalf("quota update args = %#v, want usage=100 blocked=true user=1001", quotaArgs)
	}
}

func TestTrafficStoreRecordsPendingBatchIdempotently(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	batch, err := trafficstats.NewPendingBatch(start.Add(time.Minute), []trafficstats.Sample{{TelegramID: 1001, Uplink: 1}})
	if err != nil {
		t.Fatalf("NewPendingBatch() error = %v", err)
	}
	transaction := &trafficTransactionStub{rows: []pgx.Row{
		&trafficRowStub{values: []any{true}},
		&trafficRowStub{values: []any{
			int64(1001), true, "active", int64(1),
			sql.NullTime{Time: start, Valid: true}, sql.NullTime{Time: start, Valid: true},
			start, int64(2592000), int64(100), int64(0), false,
		}},
	}}
	store := NewTrafficStore(&trafficTransactionRunnerStub{transaction: transaction})

	result, err := store.RecordPendingBatch(context.Background(), batch)
	if err != nil {
		t.Fatalf("RecordPendingBatch() error = %v", err)
	}
	if result.Applied != 1 || len(transaction.querySQL) != 2 || !strings.Contains(transaction.querySQL[0], "traffic_ingestion_batches") {
		t.Fatalf("RecordPendingBatch() = %#v queries=%#v", result, transaction.querySQL)
	}
}

func TestTrafficStoreSkipsAlreadyCommittedPendingBatch(t *testing.T) {
	batch, err := trafficstats.NewPendingBatch(time.Now(), []trafficstats.Sample{{TelegramID: 1001, Uplink: 1}})
	if err != nil {
		t.Fatalf("NewPendingBatch() error = %v", err)
	}
	transaction := &trafficTransactionStub{rows: []pgx.Row{&trafficRowStub{values: []any{false}}}}
	runner := &trafficTransactionRunnerStub{transaction: transaction}
	store := NewTrafficStore(runner)

	result, err := store.RecordPendingBatch(context.Background(), batch)
	if err != nil {
		t.Fatalf("RecordPendingBatch() error = %v", err)
	}
	if !reflect.DeepEqual(result, TrafficBatchResult{}) || len(transaction.querySQL) != 1 || len(transaction.execSQL) != 0 || !runner.committed {
		t.Fatalf("duplicate result=%#v queries=%#v writes=%#v committed=%v", result, transaction.querySQL, transaction.execSQL, runner.committed)
	}
}

func TestTrafficStoreSkipsUserThatIsNoLongerActive(t *testing.T) {
	transaction := &trafficTransactionStub{rows: []pgx.Row{&trafficRowStub{err: pgx.ErrNoRows}}}
	runner := &trafficTransactionRunnerStub{transaction: transaction}
	store := NewTrafficStore(runner)
	result, err := store.RecordBatch(context.Background(), time.Now(), []trafficstats.Sample{{TelegramID: 1001, Uplink: 1}})
	if err != nil {
		t.Fatalf("RecordBatch() error = %v", err)
	}
	if result.Applied != 0 || len(transaction.execSQL) != 0 {
		t.Fatalf("RecordBatch() = %#v writes=%#v, want skipped", result, transaction.execSQL)
	}
}

func TestTrafficStoreRollsBackWholeBatchOnPersistenceFailure(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	row := func(id int64) pgx.Row {
		return &trafficRowStub{values: []any{
			id, true, "active", int64(1),
			sql.NullTime{Time: start, Valid: true}, sql.NullTime{Time: start, Valid: true},
			start, int64(2592000), int64(100), int64(0), false,
		}}
	}
	wantErr := errors.New("write failed")
	transaction := &trafficTransactionStub{rows: []pgx.Row{row(1001), row(2002)}, execErrAt: 2, execErr: wantErr}
	runner := &trafficTransactionRunnerStub{transaction: transaction}
	store := NewTrafficStore(runner)
	result, err := store.RecordBatch(context.Background(), start.Add(time.Minute), []trafficstats.Sample{
		{TelegramID: 1001, Uplink: 1},
		{TelegramID: 2002, Downlink: 1},
	})
	if !errors.Is(err, wantErr) || result.Applied != 0 || len(result.RevokedTelegramIDs) != 0 || len(result.RestoredTelegramIDs) != 0 {
		t.Fatalf("RecordBatch() = (%#v, %v), want zero wrapping %v", result, err, wantErr)
	}
	if runner.committed || !runner.rolledBack {
		t.Fatalf("transaction committed=%v rolledBack=%v", runner.committed, runner.rolledBack)
	}
}

func TestTrafficStoreRejectsInvalidBatchBeforeTransaction(t *testing.T) {
	for _, test := range []struct {
		name       string
		observedAt time.Time
		samples    []trafficstats.Sample
	}{
		{name: "zero time", samples: []trafficstats.Sample{{TelegramID: 1, Uplink: 1}}},
		{name: "zero id", observedAt: time.Now(), samples: []trafficstats.Sample{{TelegramID: 0, Uplink: 1}}},
		{name: "negative", observedAt: time.Now(), samples: []trafficstats.Sample{{TelegramID: 1, Uplink: -1}}},
		{name: "not ordered", observedAt: time.Now(), samples: []trafficstats.Sample{{TelegramID: 2}, {TelegramID: 1}}},
		{name: "duplicate", observedAt: time.Now(), samples: []trafficstats.Sample{{TelegramID: 1}, {TelegramID: 1}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &trafficTransactionRunnerStub{}
			store := NewTrafficStore(runner)
			if _, err := store.RecordBatch(context.Background(), test.observedAt, test.samples); err == nil {
				t.Fatal("RecordBatch() error = nil, want error")
			}
			if runner.called {
				t.Fatal("invalid batch opened transaction")
			}
		})
	}
}

func TestTrafficStoreTransitionsGlobalFailClosedAndQueuesAllUsersAtomically(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 5, 0, 0, time.UTC)
	for _, test := range []struct {
		name       string
		failClosed bool
		action     string
	}{
		{name: "close", failClosed: true, action: "revoke"},
		{name: "recover", failClosed: false, action: "reconcile"},
	} {
		t.Run(test.name, func(t *testing.T) {
			transaction := &trafficTransactionStub{rows: []pgx.Row{&trafficRowStub{values: []any{true}}}}
			runner := &trafficTransactionRunnerStub{transaction: transaction}
			changed, err := NewTrafficStore(runner).SetFailClosed(context.Background(), test.failClosed, now)
			if err != nil {
				t.Fatalf("SetFailClosed() error = %v", err)
			}
			if !changed || !runner.committed || runner.rolledBack {
				t.Fatalf("SetFailClosed() changed=%v committed=%v rolledBack=%v", changed, runner.committed, runner.rolledBack)
			}
			if len(transaction.querySQL) != 1 || !strings.Contains(transaction.querySQL[0], "UPDATE traffic_health") {
				t.Fatalf("transition query = %#v", transaction.querySQL)
			}
			if len(transaction.execSQL) != 1 || !strings.Contains(transaction.execSQL[0], "INSERT INTO core_action_outbox") ||
				!strings.Contains(transaction.execSQL[0], "FROM vpn_users") || transaction.execArgs[0][0] != test.action {
				t.Fatalf("queued action SQL=%#v args=%#v", transaction.execSQL, transaction.execArgs)
			}
		})
	}
}

func TestTrafficStoreDoesNotQueueWhenFailClosedStateIsUnchanged(t *testing.T) {
	transaction := &trafficTransactionStub{rows: []pgx.Row{&trafficRowStub{values: []any{false}}}}
	runner := &trafficTransactionRunnerStub{transaction: transaction}
	changed, err := NewTrafficStore(runner).SetFailClosed(context.Background(), true, time.Now())
	if err != nil {
		t.Fatalf("SetFailClosed() error = %v", err)
	}
	if changed || len(transaction.execSQL) != 0 || !runner.committed {
		t.Fatalf("SetFailClosed() changed=%v writes=%#v committed=%v", changed, transaction.execSQL, runner.committed)
	}
}

func TestTrafficStoreRejectsInvalidFailClosedTransitionBeforeTransaction(t *testing.T) {
	for _, test := range []struct {
		name  string
		store *TrafficStore
		now   time.Time
	}{
		{name: "nil store", store: nil, now: time.Now()},
		{name: "nil runner", store: NewTrafficStore(nil), now: time.Now()},
		{name: "zero time", store: NewTrafficStore(&trafficTransactionRunnerStub{})},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.store.SetFailClosed(context.Background(), true, test.now); err == nil {
				t.Fatal("SetFailClosed() error = nil")
			}
		})
	}
}

func TestTrafficStoreObservesPersistentFailureAndQueuesFailClosedTransition(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 5, 0, 0, time.UTC)
	started := now.Add(-5 * time.Minute)
	transaction := &trafficTransactionStub{rows: []pgx.Row{&trafficRowStub{values: []any{started, true, true, true}}}}
	runner := &trafficTransactionRunnerStub{transaction: transaction}

	observation, err := NewTrafficStore(runner).ObserveFailure(context.Background(), "record", now)
	if err != nil {
		t.Fatalf("ObserveFailure() error = %v", err)
	}
	if observation.StartedAt != started || !observation.FailClosed || !observation.Notify || !runner.committed {
		t.Fatalf("ObserveFailure() = %#v committed=%v", observation, runner.committed)
	}
	if len(transaction.execArgs) != 1 || transaction.execArgs[0][0] != "revoke" {
		t.Fatalf("queued actions = %#v", transaction.execArgs)
	}
}

func TestTrafficStoreObservesRecoveryAndQueuesReconcile(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 6, 0, 0, time.UTC)
	started := now.Add(-6 * time.Minute)
	transaction := &trafficTransactionStub{rows: []pgx.Row{&trafficRowStub{values: []any{started, true}}}}
	runner := &trafficTransactionRunnerStub{transaction: transaction}

	recovery, err := NewTrafficStore(runner).ObserveRecovery(context.Background(), now)
	if err != nil {
		t.Fatalf("ObserveRecovery() error = %v", err)
	}
	if !recovery.Recovered || !recovery.WasFailClosed || recovery.StartedAt != started {
		t.Fatalf("ObserveRecovery() = %#v", recovery)
	}
	if len(transaction.execArgs) != 1 || transaction.execArgs[0][0] != "reconcile" {
		t.Fatalf("queued actions = %#v", transaction.execArgs)
	}
}

func TestTrafficStoreRecoveryWithoutFailureIsNoop(t *testing.T) {
	transaction := &trafficTransactionStub{rows: []pgx.Row{&trafficRowStub{err: pgx.ErrNoRows}}}
	runner := &trafficTransactionRunnerStub{transaction: transaction}
	recovery, err := NewTrafficStore(runner).ObserveRecovery(context.Background(), time.Now())
	if err != nil || recovery.Recovered || len(transaction.execSQL) != 0 {
		t.Fatalf("ObserveRecovery() = %#v error=%v writes=%#v", recovery, err, transaction.execSQL)
	}
}

func TestTrafficStoreRejectsUnknownFailureStageBeforeTransaction(t *testing.T) {
	runner := &trafficTransactionRunnerStub{}
	if _, err := NewTrafficStore(runner).ObserveFailure(context.Background(), "database password", time.Now()); err == nil {
		t.Fatal("ObserveFailure() error = nil")
	}
	if runner.called {
		t.Fatal("invalid failure stage opened transaction")
	}
}

func TestTrafficStoreAdvancesDueQuotaPeriodsAndQueuesRestoreAtomically(t *testing.T) {
	now := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	transaction := &trafficTransactionStub{rows: []pgx.Row{&trafficRowStub{values: []any{int64(2), int64(1)}}}}
	runner := &trafficTransactionRunnerStub{transaction: transaction}

	advanced, restored, err := NewTrafficStore(runner).AdvanceDueQuotaPeriods(context.Background(), now, 200)
	if err != nil {
		t.Fatalf("AdvanceDueQuotaPeriods() error = %v", err)
	}
	if advanced != 2 || restored != 1 || !runner.committed || runner.rolledBack {
		t.Fatalf("AdvanceDueQuotaPeriods() = (%d, %d), committed=%v rolledBack=%v", advanced, restored, runner.committed, runner.rolledBack)
	}
	if len(transaction.querySQL) != 1 || !strings.Contains(transaction.querySQL[0], "FOR UPDATE OF quota SKIP LOCKED") ||
		!strings.Contains(transaction.querySQL[0], "used_bytes = 0") ||
		!strings.Contains(transaction.querySQL[0], "core_action_outbox") ||
		!strings.Contains(transaction.querySQL[0], "traffic_health.fail_closed = FALSE") {
		t.Fatalf("quota sweep query = %q", transaction.querySQL)
	}
}

func TestTrafficStoreRejectsInvalidQuotaSweepBeforeTransaction(t *testing.T) {
	for _, test := range []struct {
		name  string
		store *TrafficStore
		now   time.Time
		limit int
	}{
		{name: "nil store", now: time.Now(), limit: 200},
		{name: "nil runner", store: NewTrafficStore(nil), now: time.Now(), limit: 200},
		{name: "zero time", store: NewTrafficStore(&trafficTransactionRunnerStub{}), limit: 200},
		{name: "zero limit", store: NewTrafficStore(&trafficTransactionRunnerStub{}), now: time.Now()},
		{name: "oversized limit", store: NewTrafficStore(&trafficTransactionRunnerStub{}), now: time.Now(), limit: 1001},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := test.store.AdvanceDueQuotaPeriods(context.Background(), test.now, test.limit); err == nil {
				t.Fatal("AdvanceDueQuotaPeriods() error = nil")
			}
			if test.store != nil && test.store.transactions != nil {
				runner := test.store.transactions.(*trafficTransactionRunnerStub)
				if runner.called {
					t.Fatal("invalid sweep opened transaction")
				}
			}
		})
	}
}

type trafficTransactionRunnerStub struct {
	transaction *trafficTransactionStub
	called      bool
	committed   bool
	rolledBack  bool
}

func (runner *trafficTransactionRunnerStub) RunInTransaction(ctx context.Context, operation func(Database) error) error {
	runner.called = true
	err := operation(runner.transaction)
	if err != nil {
		runner.rolledBack = true
		return err
	}
	runner.committed = true
	return nil
}

type trafficTransactionStub struct {
	rows      []pgx.Row
	querySQL  []string
	execSQL   []string
	execArgs  [][]any
	execErrAt int
	execErr   error
}

func (stub *trafficTransactionStub) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	stub.querySQL = append(stub.querySQL, query)
	row := stub.rows[0]
	stub.rows = stub.rows[1:]
	return row
}

func (stub *trafficTransactionStub) Exec(_ context.Context, query string, arguments ...any) (pgconn.CommandTag, error) {
	stub.execSQL = append(stub.execSQL, query)
	stub.execArgs = append(stub.execArgs, arguments)
	if stub.execErr != nil && len(stub.execSQL)-1 == stub.execErrAt {
		return pgconn.CommandTag{}, stub.execErr
	}
	return pgconn.CommandTag{}, nil
}

type trafficRowStub struct {
	values []any
	err    error
}

func (row *trafficRowStub) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != len(row.values) {
		return errors.New("unexpected traffic destination count")
	}
	for index, destination := range destinations {
		switch pointer := destination.(type) {
		case *int64:
			*pointer = row.values[index].(int64)
		case *bool:
			*pointer = row.values[index].(bool)
		case *string:
			*pointer = row.values[index].(string)
		case *sql.NullTime:
			*pointer = row.values[index].(sql.NullTime)
		case *time.Time:
			*pointer = row.values[index].(time.Time)
		default:
			return errors.New("unsupported traffic scan destination")
		}
	}
	return nil
}
