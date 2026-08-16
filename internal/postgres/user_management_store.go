package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

var ErrVPNUserNotFound = errors.New("VPN user not found")

type UserManagementStore struct {
	transactions TransactionRunner
	database     Database
}

func NewUserManagementStore(transactions TransactionRunner, databases ...Database) *UserManagementStore {
	var database Database
	if len(databases) > 0 {
		database = databases[0]
	}
	return &UserManagementStore{transactions: transactions, database: database}
}

func (store *UserManagementStore) ListUsers(ctx context.Context, after int64, limit int) ([]domain.UserOverview, error) {
	if store == nil || store.database == nil {
		return nil, errors.New("management database is required")
	}
	if after < 0 || limit < 1 || limit > 200 {
		return nil, errors.New("user list window is invalid")
	}
	var payload []byte
	if err := store.database.QueryRow(ctx, `
		SELECT COALESCE(jsonb_agg(jsonb_build_object(
			'telegram_id', page.telegram_id,
			'eligible', page.eligible,
			'status', page.status,
			'generation', page.credential_generation,
			'period_started_at', page.period_started_at,
			'last_vpn_activity_at', page.last_vpn_activity_at,
			'used_bytes', COALESCE(page.used_bytes, 0),
			'limit_bytes', COALESCE(page.limit_bytes, 0),
			'quota_blocked', COALESCE(page.blocked, FALSE)
		) ORDER BY page.telegram_id), '[]'::jsonb)
		FROM (
			SELECT vpn_user.*, quota.used_bytes, quota.limit_bytes, quota.blocked
			FROM vpn_users AS vpn_user
			LEFT JOIN quota_windows AS quota USING (telegram_id)
			WHERE vpn_user.telegram_id > $1
			ORDER BY vpn_user.telegram_id
			LIMIT $2
		) AS page`, after, limit).Scan(&payload); err != nil {
		return nil, fmt.Errorf("list VPN users: %w", err)
	}
	var users []domain.UserOverview
	if err := json.Unmarshal(payload, &users); err != nil {
		return nil, errors.New("persisted VPN user list is invalid")
	}
	previous := after
	for _, user := range users {
		if user.TelegramID <= previous || user.Generation > uint64(^uint64(0)>>1) || user.UsedBytes < 0 || user.LimitBytes < 0 {
			return nil, errors.New("persisted VPN user list is invalid")
		}
		switch user.Status {
		case domain.AccessStatusUnclaimed, domain.AccessStatusActive, domain.AccessStatusPendingApproval,
			domain.AccessStatusApprovalRejected, domain.AccessStatusSelfService, domain.AccessStatusPermanentlyBlocked:
		default:
			return nil, errors.New("persisted VPN user list is invalid")
		}
		previous = user.TelegramID
	}
	return users, nil
}

func (store *UserManagementStore) FindUser(ctx context.Context, telegramID int64) (domain.UserOverview, error) {
	if store == nil || store.database == nil {
		return domain.UserOverview{}, errors.New("management database is required")
	}
	if telegramID <= 0 {
		return domain.UserOverview{}, errors.New("Telegram ID must be positive")
	}
	var payload []byte
	err := store.database.QueryRow(ctx, `
		SELECT jsonb_build_object(
			'telegram_id', vpn_user.telegram_id,
			'eligible', vpn_user.eligible,
			'status', vpn_user.status,
			'generation', vpn_user.credential_generation,
			'period_started_at', vpn_user.period_started_at,
			'last_vpn_activity_at', vpn_user.last_vpn_activity_at,
			'used_bytes', COALESCE(quota.used_bytes, 0),
			'limit_bytes', COALESCE(quota.limit_bytes, 0),
			'quota_blocked', COALESCE(quota.blocked, FALSE)
		)
		FROM vpn_users AS vpn_user
		LEFT JOIN quota_windows AS quota USING (telegram_id)
		WHERE vpn_user.telegram_id = $1`, telegramID).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.UserOverview{}, ErrVPNUserNotFound
	}
	if err != nil {
		return domain.UserOverview{}, fmt.Errorf("find VPN user: %w", err)
	}
	var user domain.UserOverview
	if err := json.Unmarshal(payload, &user); err != nil || !validUserOverview(user, 0) || user.TelegramID != telegramID {
		return domain.UserOverview{}, errors.New("persisted VPN user is invalid")
	}
	return user, nil
}

func validUserOverview(user domain.UserOverview, previous int64) bool {
	if user.TelegramID <= previous || user.Generation > uint64(^uint64(0)>>1) || user.UsedBytes < 0 || user.LimitBytes < 0 {
		return false
	}
	switch user.Status {
	case domain.AccessStatusUnclaimed, domain.AccessStatusActive, domain.AccessStatusPendingApproval,
		domain.AccessStatusApprovalRejected, domain.AccessStatusSelfService, domain.AccessStatusPermanentlyBlocked:
		return true
	default:
		return false
	}
}

func (store *UserManagementStore) Revoke(ctx context.Context, actorID, telegramID int64, mode domain.RevocationMode, now time.Time) error {
	if actorID <= 0 || telegramID <= 0 || now.IsZero() {
		return errors.New("management operation input is invalid")
	}
	if mode != domain.RevocationModeSelfService && mode != domain.RevocationModeRequireApproval && mode != domain.RevocationModePermanentBlock {
		return errors.New("revocation mode is invalid")
	}
	return store.update(ctx, actorID, telegramID, now, "vpn.revoke", func(account *domain.AccessAccount) (bool, error) {
		change, err := account.Revoke(mode)
		return change.RevokeCredentialsImmediately, err
	})
}

func (store *UserManagementStore) RejectApproval(ctx context.Context, actorID, telegramID int64, now time.Time) error {
	if actorID <= 0 || telegramID <= 0 || now.IsZero() {
		return errors.New("management operation input is invalid")
	}
	return store.update(ctx, actorID, telegramID, now, "vpn.reject", func(account *domain.AccessAccount) (bool, error) {
		return false, account.RejectApproval()
	})
}

func (store *UserManagementStore) update(ctx context.Context, actorID, telegramID int64, now time.Time, action string, mutate func(*domain.AccessAccount) (bool, error)) error {
	if store == nil || store.transactions == nil || mutate == nil {
		return errors.New("management store is not initialized")
	}
	return store.transactions.RunInTransaction(ctx, func(transaction Database) error {
		snapshot, err := lockAccessSnapshot(ctx, transaction, telegramID)
		if err != nil {
			return err
		}
		account, err := domain.RestoreAccessAccount(snapshot)
		if err != nil {
			return fmt.Errorf("restore VPN user: %w", err)
		}
		revoke, err := mutate(account)
		if err != nil {
			return err
		}
		updated := account.Snapshot()
		if _, err := transaction.Exec(ctx, `
			UPDATE vpn_users SET status = $1, updated_at = $2 WHERE telegram_id = $3`,
			string(updated.Status), now, telegramID); err != nil {
			return fmt.Errorf("persist managed VPN user: %w", err)
		}
		if revoke {
			if _, err := transaction.Exec(ctx, `
				INSERT INTO core_action_outbox (telegram_id, action, available_at)
				VALUES ($1, 'revoke', $2)
				ON CONFLICT (telegram_id, action) WHERE completed_at IS NULL DO NOTHING`, telegramID, now); err != nil {
				return fmt.Errorf("queue managed revocation: %w", err)
			}
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO audit_events (actor_telegram_id, action, target_type, target_id, details, created_at)
			VALUES ($1, $2, 'vpn_user', $3, jsonb_build_object('status', $4), $5)`,
			actorID, action, fmt.Sprint(telegramID), string(updated.Status), now); err != nil {
			return fmt.Errorf("record managed VPN audit event: %w", err)
		}
		return nil
	})
}
