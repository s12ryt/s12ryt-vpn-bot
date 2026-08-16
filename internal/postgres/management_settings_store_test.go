package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

func TestManagementSettingsStoreReadsSettingsAndRules(t *testing.T) {
	database := &managementDatabaseStub{row: &managementRowStub{values: []any{[]byte(`{
		"settings":{"qualification_mode":"all","recheck_interval_minutes":120,"recheck_requests_per_second":12,"recheck_batch_size":80,"inactivity_threshold_days":7,"quota_limit_bytes":60000000000},
		"rules":[{"chat_id":-1002,"chat_type":"channel","title":"News","enabled":true,"bot_administrator_passed":true}]
	}`)}}}
	store := NewManagementSettingsStore(nil, database, nil)
	settings, rules, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if settings.QualificationMode != domain.QualificationAll || settings.QuotaLimitBytes != 60_000_000_000 {
		t.Fatalf("settings = %#v", settings)
	}
	if len(rules) != 1 || rules[0].ChatID != -1002 || rules[0].Title != "News" {
		t.Fatalf("rules = %#v", rules)
	}
	if strings.Contains(strings.ToLower(database.query), "credential") {
		t.Fatalf("settings query reads credentials: %s", database.query)
	}
}

func TestManagementSettingsStoreUpdatesQuotaAndInactivityAtomically(t *testing.T) {
	now := time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
	transaction := &accessTransactionStub{row: &managementRowStub{values: []any{0}}}
	runner := &transactionRunnerStub{transaction: transaction}
	trigger := &recheckTriggerStub{}
	store := NewManagementSettingsStore(runner, nil, trigger)
	settings := validManagementSettings()
	settings.InactivityThresholdDays = 7
	settings.QuotaLimitBytes = 40_000_000_000

	if err := store.Update(context.Background(), 9001, settings, true, now); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !runner.committed || runner.rolledBack || !trigger.called {
		t.Fatalf("committed=%v rolledBack=%v triggered=%v", runner.committed, runner.rolledBack, trigger.called)
	}
	if len(transaction.execSQL) != 4 {
		t.Fatalf("Exec calls = %d, want settings, quota, inactivity, audit", len(transaction.execSQL))
	}
	if !strings.Contains(transaction.execSQL[1], "quota_windows") || !strings.Contains(transaction.execSQL[1], "core_action_outbox") {
		t.Fatalf("quota transition query = %q", transaction.execSQL[1])
	}
	if !strings.Contains(transaction.execSQL[2], "last_vpn_activity_at") || !strings.Contains(transaction.execSQL[2], "revoke") {
		t.Fatalf("inactivity transition query = %q", transaction.execSQL[2])
	}
	if !strings.Contains(transaction.execSQL[3], "audit_events") {
		t.Fatalf("audit query = %q", transaction.execSQL[3])
	}
}

func TestManagementSettingsStoreRequiresConfirmationBeforeLoweringInactivityThreshold(t *testing.T) {
	transaction := &accessTransactionStub{row: &managementRowStub{values: []any{30}}}
	runner := &transactionRunnerStub{transaction: transaction}
	store := NewManagementSettingsStore(runner, nil, &recheckTriggerStub{})
	settings := validManagementSettings()
	settings.InactivityThresholdDays = 7

	err := store.Update(context.Background(), 9001, settings, false, time.Now().UTC())
	if !errors.Is(err, ErrInactivityConfirmationRequired) {
		t.Fatalf("Update() error = %v", err)
	}
	if runner.committed || len(transaction.execSQL) != 0 {
		t.Fatalf("unconfirmed update committed=%v exec=%d", runner.committed, len(transaction.execSQL))
	}
}

func TestManagementSettingsStoreRejectsInvalidInputBeforeTransaction(t *testing.T) {
	runner := &transactionRunnerStub{}
	store := NewManagementSettingsStore(runner, nil, &recheckTriggerStub{})
	settings := validManagementSettings()
	settings.RecheckBatchSize = 1
	if err := store.Update(context.Background(), 9001, settings, false, time.Now().UTC()); err == nil || runner.called {
		t.Fatalf("invalid update error=%v transaction=%v", err, runner.called)
	}
}

func TestManagementSettingsStorePreviewsInactiveUsers(t *testing.T) {
	database := &managementDatabaseStub{row: &managementRowStub{values: []any{int64(12)}}}
	store := NewManagementSettingsStore(nil, database, nil)
	count, err := store.PreviewInactivity(context.Background(), 7, time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("PreviewInactivity() error = %v", err)
	}
	if count != 12 || !strings.Contains(database.query, "last_vpn_activity_at") || !strings.Contains(database.query, "status = 'active'") {
		t.Fatalf("count=%d query=%q", count, database.query)
	}
	if _, err := store.PreviewInactivity(context.Background(), 0, time.Now().UTC()); err == nil {
		t.Fatal("zero threshold accepted")
	}
}

func validManagementSettings() domain.ManagementSettings {
	return domain.ManagementSettings{
		QualificationMode:        domain.QualificationAny,
		RecheckIntervalMinutes:   60,
		RecheckRequestsPerSecond: 10,
		RecheckBatchSize:         50,
		QuotaLimitBytes:          50_000_000_000,
	}
}

type recheckTriggerStub struct{ called bool }

func (trigger *recheckTriggerStub) Trigger() { trigger.called = true }

type managementDatabaseStub struct {
	query string
	row   pgx.Row
}

func (stub *managementDatabaseStub) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (stub *managementDatabaseStub) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	stub.query = query
	return stub.row
}

type managementRowStub struct {
	values []any
	err    error
}

func (stub *managementRowStub) Scan(destinations ...any) error {
	if stub.err != nil {
		return stub.err
	}
	if len(destinations) != len(stub.values) {
		return errors.New("unexpected scan destination count")
	}
	for index, destination := range destinations {
		switch pointer := destination.(type) {
		case *int:
			*pointer = stub.values[index].(int)
		case *int64:
			*pointer = stub.values[index].(int64)
		case *[]byte:
			*pointer = stub.values[index].([]byte)
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}
