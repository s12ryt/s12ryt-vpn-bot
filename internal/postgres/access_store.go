package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

type TransactionRunner interface {
	RunInTransaction(ctx context.Context, operation func(Database) error) error
}

type AccessStore struct {
	transactions TransactionRunner
}

func NewAccessStore(transactions TransactionRunner) *AccessStore {
	return &AccessStore{transactions: transactions}
}

func (store *AccessStore) ApplyQualification(
	ctx context.Context,
	telegramID int64,
	decision domain.QualificationDecision,
) (domain.AccessChange, error) {
	if store == nil || store.transactions == nil {
		return domain.AccessChange{}, errors.New("access transaction runner is required")
	}
	if telegramID <= 0 {
		return domain.AccessChange{}, errors.New("Telegram ID must be positive")
	}
	if decision == domain.QualificationIndeterminate {
		return domain.AccessChange{}, nil
	}
	if decision != domain.QualificationEligible && decision != domain.QualificationIneligible {
		return domain.AccessChange{}, errors.New("qualification decision is invalid")
	}

	var change domain.AccessChange
	err := store.transactions.RunInTransaction(ctx, func(transaction Database) error {
		if transaction == nil {
			return errors.New("access transaction is required")
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO vpn_users (telegram_id)
			VALUES ($1)
			ON CONFLICT (telegram_id) DO NOTHING`, telegramID); err != nil {
			return fmt.Errorf("ensure VPN user: %w", err)
		}

		snapshot, err := lockAccessSnapshot(ctx, transaction, telegramID)
		if err != nil {
			return err
		}
		account, err := domain.RestoreAccessAccount(snapshot)
		if err != nil {
			return fmt.Errorf("restore VPN user: %w", err)
		}
		change, err = domain.ApplyQualification(account, decision)
		if err != nil {
			return fmt.Errorf("apply qualification: %w", err)
		}
		updated := account.Snapshot()
		if _, err := transaction.Exec(ctx, `
			UPDATE vpn_users
			SET eligible = $1,
			    status = $2,
			    credential_generation = $3,
			    period_started_at = $4,
			    last_vpn_activity_at = $5,
			    updated_at = NOW()
			WHERE telegram_id = $6`,
			updated.Eligible,
			string(updated.Status),
			int64(updated.CredentialGeneration),
			nullableTime(updated.PeriodStartedAt),
			nullableTime(updated.LastVPNActivityAt),
			updated.TelegramID,
		); err != nil {
			return fmt.Errorf("persist VPN user qualification: %w", err)
		}
		if change.RevokeCredentialsImmediately {
			if _, err := transaction.Exec(ctx, `
				INSERT INTO core_action_outbox (telegram_id, action)
				VALUES ($1, 'revoke')
				ON CONFLICT (telegram_id, action) WHERE completed_at IS NULL DO NOTHING`,
				updated.TelegramID,
			); err != nil {
				return fmt.Errorf("queue credential revocation: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return domain.AccessChange{}, err
	}
	return change, nil
}

func lockAccessSnapshot(ctx context.Context, transaction Database, telegramID int64) (domain.AccessSnapshot, error) {
	row := transaction.QueryRow(ctx, `
		SELECT telegram_id, eligible, status, credential_generation,
		       period_started_at, last_vpn_activity_at
		FROM vpn_users
		WHERE telegram_id = $1
		FOR UPDATE`, telegramID)
	var snapshot domain.AccessSnapshot
	var rawStatus string
	var generation int64
	var periodStartedAt sql.NullTime
	var lastVPNActivityAt sql.NullTime
	if err := row.Scan(
		&snapshot.TelegramID,
		&snapshot.Eligible,
		&rawStatus,
		&generation,
		&periodStartedAt,
		&lastVPNActivityAt,
	); err != nil {
		return domain.AccessSnapshot{}, fmt.Errorf("lock VPN user: %w", err)
	}
	if generation < 0 {
		return domain.AccessSnapshot{}, errors.New("persisted credential generation is invalid")
	}
	snapshot.Status = domain.AccessStatus(rawStatus)
	snapshot.CredentialGeneration = uint64(generation)
	if periodStartedAt.Valid {
		snapshot.PeriodStartedAt = periodStartedAt.Time
	}
	if lastVPNActivityAt.Valid {
		snapshot.LastVPNActivityAt = lastVPNActivityAt.Time
	}
	return snapshot, nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
