package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
)

type Database interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, arguments ...any) pgx.Row
}

type AuthStore struct {
	database Database
}

func NewAuthStore(database Database) *AuthStore {
	return &AuthStore{database: database}
}

func (store *AuthStore) FindActive(ctx context.Context, telegramID int64) (auth.Administrator, error) {
	row := store.database.QueryRow(ctx, `
		SELECT telegram_id, role, is_root, active
		FROM administrators
		WHERE telegram_id = $1 AND active`, telegramID)
	administrator, err := scanAdministrator(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Administrator{}, auth.ErrAdministratorUnauthorized
	}
	return administrator, err
}

func (store *AuthStore) EnsureRootOwner(ctx context.Context, telegramID int64) error {
	if telegramID <= 0 {
		return errors.New("root owner Telegram ID must be positive")
	}
	_, err := store.database.Exec(ctx, `
		INSERT INTO administrators (telegram_id, role, is_root, active)
		VALUES ($1, 'owner', TRUE, TRUE)
		ON CONFLICT (telegram_id) DO UPDATE
		SET role = 'owner', is_root = TRUE, active = TRUE`, telegramID)
	return err
}

func (store *AuthStore) ActiveAdministratorIDs(ctx context.Context) ([]int64, error) {
	row := store.database.QueryRow(ctx, `
		SELECT COALESCE(array_agg(telegram_id ORDER BY telegram_id), ARRAY[]::BIGINT[])
		FROM administrators
		WHERE active`)
	var telegramIDs []int64
	if err := row.Scan(&telegramIDs); err != nil {
		return nil, err
	}
	previous := int64(0)
	for _, telegramID := range telegramIDs {
		if telegramID <= previous {
			return nil, errors.New("persisted active administrators are invalid")
		}
		previous = telegramID
	}
	return telegramIDs, nil
}

func (store *AuthStore) Replace(ctx context.Context, record auth.LoginCodeRecord) error {
	_, err := store.database.Exec(ctx, `
		INSERT INTO admin_login_codes (telegram_id, digest, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (telegram_id) DO UPDATE
		SET digest = EXCLUDED.digest, expires_at = EXCLUDED.expires_at`,
		record.TelegramID, record.Digest[:], record.ExpiresAt)
	return err
}

func (store *AuthStore) Consume(ctx context.Context, telegramID int64, digest [32]byte, now time.Time) (auth.Administrator, error) {
	row := store.database.QueryRow(ctx, `
		DELETE FROM admin_login_codes AS code
		USING administrators AS administrator
		WHERE code.telegram_id = $1
		  AND code.telegram_id = administrator.telegram_id
		  AND code.digest = $2
		  AND code.expires_at > $3
		  AND administrator.active
		RETURNING administrator.telegram_id, administrator.role, administrator.is_root, administrator.active`,
		telegramID, digest[:], now)
	administrator, err := scanAdministrator(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Administrator{}, auth.ErrLoginCodeInvalid
	}
	return administrator, err
}

func (store *AuthStore) Create(ctx context.Context, record auth.SessionRecord) error {
	_, err := store.database.Exec(ctx, `
		INSERT INTO admin_sessions (digest, telegram_id, created_at, last_seen_at, absolute_expires_at)
		VALUES ($1, $2, $3, $4, $5)`,
		record.Digest[:], record.Administrator.TelegramID, record.CreatedAt, record.LastSeenAt, record.AbsoluteExpiresAt)
	return err
}

func (store *AuthStore) AuthenticateAndTouch(ctx context.Context, digest [32]byte, now time.Time, idleTimeout time.Duration) (auth.Administrator, error) {
	row := store.database.QueryRow(ctx, `
		UPDATE admin_sessions AS session
		SET last_seen_at = $2
		FROM administrators AS administrator
		WHERE session.telegram_id = administrator.telegram_id
		  AND session.digest = $1
		  AND session.absolute_expires_at > $2
		  AND session.last_seen_at > $3
		  AND administrator.active
		RETURNING administrator.telegram_id, administrator.role, administrator.is_root, administrator.active`,
		digest[:], now, now.Add(-idleTimeout))
	administrator, err := scanAdministrator(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Administrator{}, auth.ErrSessionInvalid
	}
	return administrator, err
}

func (store *AuthStore) Delete(ctx context.Context, digest [32]byte) error {
	_, err := store.database.Exec(ctx, `DELETE FROM admin_sessions WHERE digest = $1`, digest[:])
	return err
}

func (store *AuthStore) DeleteAll(ctx context.Context, telegramID int64) error {
	_, err := store.database.Exec(ctx, `DELETE FROM admin_sessions WHERE telegram_id = $1`, telegramID)
	return err
}

func scanAdministrator(row pgx.Row) (auth.Administrator, error) {
	var administrator auth.Administrator
	var role string
	err := row.Scan(&administrator.TelegramID, &role, &administrator.Root, &administrator.Active)
	administrator.Role = auth.Role(role)
	return administrator, err
}
