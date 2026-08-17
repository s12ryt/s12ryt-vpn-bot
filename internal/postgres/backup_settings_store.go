package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

// BackupSettingsStore exposes the retention policy to both the Web control
// plane and the isolated backup process.
type BackupSettingsStore struct {
	transactions TransactionRunner
	database     Database
}

func NewBackupSettingsStore(transactions TransactionRunner, database Database) *BackupSettingsStore {
	return &BackupSettingsStore{transactions: transactions, database: database}
}

func (store *BackupSettingsStore) Get(ctx context.Context) (domain.BackupSettings, error) {
	if store == nil || store.database == nil {
		return domain.BackupSettings{}, errors.New("backup settings database is required")
	}
	var settings domain.BackupSettings
	if err := store.database.QueryRow(ctx, `
		SELECT retention_days
		FROM backup_settings
		WHERE singleton = TRUE`).Scan(&settings.RetentionDays); err != nil {
		return domain.BackupSettings{}, fmt.Errorf("read backup settings: %w", err)
	}
	if err := settings.Validate(); err != nil {
		return domain.BackupSettings{}, fmt.Errorf("validate backup settings: %w", err)
	}
	return settings, nil
}

func (store *BackupSettingsStore) GetBackupSettings(ctx context.Context) (domain.BackupSettings, error) {
	return store.Get(ctx)
}

func (store *BackupSettingsStore) Update(ctx context.Context, actorTelegramID int64, settings domain.BackupSettings, now time.Time) error {
	if store == nil || store.transactions == nil {
		return errors.New("backup settings transaction runner is required")
	}
	if actorTelegramID <= 0 || now.IsZero() {
		return errors.New("backup settings update parameters are invalid")
	}
	if err := settings.Validate(); err != nil {
		return err
	}
	return store.transactions.RunInTransaction(ctx, func(transaction Database) error {
		if _, err := transaction.Exec(ctx, `
			UPDATE backup_settings
			SET retention_days = $1, updated_at = $2
			WHERE singleton = TRUE`, settings.RetentionDays, now); err != nil {
			return fmt.Errorf("update backup settings: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO audit_events (actor_telegram_id, action, target_type, target_id, details, created_at)
			VALUES ($1, 'backup.settings.update', 'backup', '', jsonb_build_object('retention_days', $2), $3)`,
			actorTelegramID, settings.RetentionDays, now,
		); err != nil {
			return fmt.Errorf("audit backup settings update: %w", err)
		}
		return nil
	})
}

func (store *BackupSettingsStore) UpdateBackupSettings(ctx context.Context, actorTelegramID int64, settings domain.BackupSettings, now time.Time) error {
	return store.Update(ctx, actorTelegramID, settings, now)
}
