package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
)

const administratorRoleLockID int64 = 0x733132727974726f

var ErrAdministratorNotFound = errors.New("administrator not found")

type AdministratorStore struct {
	transactions TransactionRunner
	database     Database
}

func NewAdministratorStore(transactions TransactionRunner, database Database) *AdministratorStore {
	return &AdministratorStore{transactions: transactions, database: database}
}

func (store *AdministratorStore) List(ctx context.Context) ([]auth.Administrator, error) {
	if store == nil || store.database == nil {
		return nil, errors.New("administrator database is required")
	}
	var payload []byte
	if err := store.database.QueryRow(ctx, `
		SELECT COALESCE(jsonb_agg(jsonb_build_object(
			'telegram_id', administrator.telegram_id,
			'role', administrator.role,
			'root', administrator.is_root,
			'active', administrator.active
		) ORDER BY administrator.telegram_id), '[]'::jsonb)
		FROM administrators AS administrator
		WHERE active`).Scan(&payload); err != nil {
		return nil, fmt.Errorf("list administrators: %w", err)
	}
	var rows []struct {
		TelegramID int64     `json:"telegram_id"`
		Role       auth.Role `json:"role"`
		Root       bool      `json:"root"`
		Active     bool      `json:"active"`
	}
	if err := json.Unmarshal(payload, &rows); err != nil {
		return nil, errors.New("persisted administrator list is invalid")
	}
	administrators := make([]auth.Administrator, len(rows))
	previous := int64(0)
	for index, row := range rows {
		if row.TelegramID <= previous || !row.Active || (row.Role != auth.RoleOwner && row.Role != auth.RoleAdministrator) || (row.Root && row.Role != auth.RoleOwner) {
			return nil, errors.New("persisted administrator list is invalid")
		}
		administrators[index] = auth.Administrator{TelegramID: row.TelegramID, Role: row.Role, Root: row.Root, Active: true}
		previous = row.TelegramID
	}
	return administrators, nil
}

func (store *AdministratorStore) SetRole(ctx context.Context, actorID, targetID int64, role auth.Role, now time.Time) error {
	if actorID <= 0 || targetID <= 0 || now.IsZero() || (role != auth.RoleOwner && role != auth.RoleAdministrator) {
		return auth.ErrInvalidRoleChange
	}
	return store.change(ctx, actorID, targetID, &role, now)
}

func (store *AdministratorStore) Remove(ctx context.Context, actorID, targetID int64, now time.Time) error {
	if actorID <= 0 || targetID <= 0 || now.IsZero() {
		return auth.ErrInvalidRoleChange
	}
	return store.change(ctx, actorID, targetID, nil, now)
}

func (store *AdministratorStore) change(ctx context.Context, actorID, targetID int64, newRole *auth.Role, now time.Time) error {
	if store == nil || store.transactions == nil {
		return errors.New("administrator store is not initialized")
	}
	return store.transactions.RunInTransaction(ctx, func(transaction Database) error {
		if _, err := transaction.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, administratorRoleLockID); err != nil {
			return fmt.Errorf("lock administrator roles: %w", err)
		}
		actor, target, ownerCount, err := loadRoleChangeState(ctx, transaction, actorID, targetID)
		if err != nil {
			return err
		}
		if ownerCount > math.MaxInt {
			return auth.ErrInvalidRoleChange
		}
		if newRole == nil && target.TelegramID == 0 {
			return ErrAdministratorNotFound
		}
		if err := auth.ValidateRoleChange(actor.Role, target.Role, target.Root, int(ownerCount), newRole); err != nil {
			return err
		}
		if newRole != nil {
			if _, err := transaction.Exec(ctx, `
				INSERT INTO administrators (telegram_id, role, is_root, active, updated_at)
				VALUES ($1, $2, FALSE, TRUE, $3)
				ON CONFLICT (telegram_id) DO UPDATE
				SET role = EXCLUDED.role, active = TRUE, updated_at = EXCLUDED.updated_at`,
				targetID, string(*newRole), now); err != nil {
				return fmt.Errorf("persist administrator role: %w", err)
			}
		} else {
			if _, err := transaction.Exec(ctx, `UPDATE administrators SET active = FALSE, updated_at = $2 WHERE telegram_id = $1`, targetID, now); err != nil {
				return fmt.Errorf("remove administrator: %w", err)
			}
			if _, err := transaction.Exec(ctx, `DELETE FROM admin_login_codes WHERE telegram_id = $1`, targetID); err != nil {
				return fmt.Errorf("revoke administrator login code: %w", err)
			}
			if _, err := transaction.Exec(ctx, `DELETE FROM admin_sessions WHERE telegram_id = $1`, targetID); err != nil {
				return fmt.Errorf("revoke administrator sessions: %w", err)
			}
		}
		action, role := "administrator.remove", ""
		if newRole != nil {
			action, role = "administrator.set_role", string(*newRole)
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO audit_events (actor_telegram_id, action, target_type, target_id, details, created_at)
			VALUES ($1, $2, 'administrator', $3, jsonb_build_object('role', $4), $5)`,
			actorID, action, fmt.Sprint(targetID), role, now); err != nil {
			return fmt.Errorf("record administrator audit event: %w", err)
		}
		return nil
	})
}

func loadRoleChangeState(ctx context.Context, database Database, actorID, targetID int64) (auth.Administrator, auth.Administrator, int64, error) {
	var actor auth.Administrator
	var targetRole sql.NullString
	var targetRoot, targetActive sql.NullBool
	var ownerCount int64
	var actorRole string
	err := database.QueryRow(ctx, `
		SELECT actor.role, actor.is_root, actor.active,
		       target.role, target.is_root, target.active,
		       (SELECT COUNT(*) FROM administrators WHERE role = 'owner' AND active)
		FROM administrators AS actor
		LEFT JOIN administrators AS target ON target.telegram_id = $2
		WHERE actor.telegram_id = $1 AND actor.active`, actorID, targetID).Scan(
		&actorRole, &actor.Root, &actor.Active,
		&targetRole, &targetRoot, &targetActive, &ownerCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Administrator{}, auth.Administrator{}, 0, auth.ErrRoleManagementForbidden
	}
	if err != nil {
		return auth.Administrator{}, auth.Administrator{}, 0, fmt.Errorf("load administrator role state: %w", err)
	}
	actor.TelegramID, actor.Role = actorID, auth.Role(actorRole)
	target := auth.Administrator{}
	if targetRole.Valid {
		if !targetRoot.Valid || !targetActive.Valid {
			return auth.Administrator{}, auth.Administrator{}, 0, auth.ErrInvalidRoleChange
		}
		target = auth.Administrator{TelegramID: targetID, Role: auth.Role(targetRole.String), Root: targetRoot.Bool, Active: targetActive.Bool}
	}
	return actor, target, ownerCount, nil
}
