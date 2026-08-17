package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

func TestBackupSettingsStoreReadsRetention(t *testing.T) {
	database := &managementDatabaseStub{row: &managementRowStub{values: []any{30}}}
	store := NewBackupSettingsStore(nil, database)

	settings, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if settings.RetentionDays != 30 || !strings.Contains(database.query, "backup_settings") {
		t.Fatalf("settings=%#v query=%q", settings, database.query)
	}
}

func TestBackupSettingsStoreUpdatesRetentionWithAudit(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	transaction := &accessTransactionStub{}
	runner := &transactionRunnerStub{transaction: transaction}
	store := NewBackupSettingsStore(runner, nil)

	if err := store.Update(context.Background(), 77, domain.BackupSettings{RetentionDays: 14}, now); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !runner.committed || len(transaction.execSQL) != 2 {
		t.Fatalf("committed=%v exec=%d", runner.committed, len(transaction.execSQL))
	}
	if !strings.Contains(transaction.execSQL[0], "backup_settings") || !strings.Contains(transaction.execSQL[1], "backup.settings.update") {
		t.Fatalf("queries=%q", transaction.execSQL)
	}
}

func TestBackupSettingsStoreRejectsInvalidUpdateBeforeTransaction(t *testing.T) {
	runner := &transactionRunnerStub{}
	store := NewBackupSettingsStore(runner, nil)
	if err := store.Update(context.Background(), 0, domain.BackupSettings{RetentionDays: 7}, time.Now().UTC()); err == nil || runner.called {
		t.Fatalf("error=%v transaction=%v", err, runner.called)
	}
	if err := store.Update(context.Background(), 77, domain.BackupSettings{RetentionDays: 0}, time.Now().UTC()); err == nil || runner.called {
		t.Fatalf("error=%v transaction=%v", err, runner.called)
	}
}
