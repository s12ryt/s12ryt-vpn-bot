package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/qualification"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
)

type QualificationStore struct {
	database Database
}

func NewQualificationStore(database Database) *QualificationStore {
	return &QualificationStore{database: database}
}

func (store *QualificationStore) ActiveRules(ctx context.Context) (domain.QualificationMode, []qualification.Rule, error) {
	if store == nil || store.database == nil {
		return "", nil, errors.New("qualification database is required")
	}
	row := store.database.QueryRow(ctx, `
		SELECT setting.mode,
		       COALESCE(
		           array_agg(rule.chat_id ORDER BY rule.chat_id) FILTER (WHERE rule.chat_id IS NOT NULL),
		           ARRAY[]::BIGINT[]
		       )
		FROM qualification_settings AS setting
		LEFT JOIN qualification_rules AS rule ON rule.enabled
		WHERE setting.singleton
		GROUP BY setting.mode`)
	var rawMode string
	var chatIDs []int64
	if err := row.Scan(&rawMode, &chatIDs); err != nil {
		return "", nil, fmt.Errorf("query active qualification rules: %w", err)
	}
	mode := domain.QualificationMode(rawMode)
	if mode != domain.QualificationAny && mode != domain.QualificationAll {
		return "", nil, errors.New("persisted qualification mode is invalid")
	}
	rules := make([]qualification.Rule, len(chatIDs))
	for index, chatID := range chatIDs {
		if chatID == 0 {
			return "", nil, errors.New("persisted qualification rule is invalid")
		}
		rules[index] = qualification.Rule{ChatID: chatID}
	}
	return mode, rules, nil
}

func (store *QualificationStore) UpsertVerified(ctx context.Context, rule qualification.ManagedRule, verifiedAt time.Time) error {
	if store == nil || store.database == nil {
		return errors.New("qualification database is required")
	}
	if rule.ChatID == 0 ||
		(rule.ChatType != telegram.ChatSupergroup && rule.ChatType != telegram.ChatChannel) ||
		verifiedAt.IsZero() {
		return errors.New("verified qualification rule is invalid")
	}
	_, err := store.database.Exec(ctx, `
		INSERT INTO qualification_rules (chat_id, chat_type, title, enabled, bot_admin_verified_at)
		VALUES ($1, $2, $3, TRUE, $4)
		ON CONFLICT (chat_id) DO UPDATE
		SET chat_type = EXCLUDED.chat_type,
		    title = EXCLUDED.title,
		    enabled = TRUE,
		    bot_admin_verified_at = EXCLUDED.bot_admin_verified_at,
		    updated_at = NOW()`,
		rule.ChatID, string(rule.ChatType), strings.TrimSpace(rule.Title), verifiedAt)
	return err
}

func (store *QualificationStore) KnownUsersAfter(ctx context.Context, afterTelegramID int64, limit int) ([]int64, error) {
	if store == nil || store.database == nil {
		return nil, errors.New("qualification database is required")
	}
	if afterTelegramID < 0 {
		return nil, errors.New("known user cursor cannot be negative")
	}
	if limit < 10 || limit > 200 {
		return nil, errors.New("known user page limit must be between 10 and 200")
	}
	row := store.database.QueryRow(ctx, `
		SELECT COALESCE(array_agg(page.telegram_id ORDER BY page.telegram_id), ARRAY[]::BIGINT[])
		FROM (
			SELECT telegram_id
			FROM vpn_users
			WHERE telegram_id > $1
			ORDER BY telegram_id
			LIMIT $2
		) AS page`, afterTelegramID, limit)
	var telegramIDs []int64
	if err := row.Scan(&telegramIDs); err != nil {
		return nil, fmt.Errorf("query known VPN users: %w", err)
	}
	previous := afterTelegramID
	for _, telegramID := range telegramIDs {
		if telegramID <= previous {
			return nil, errors.New("persisted VPN users are not strictly ordered")
		}
		previous = telegramID
	}
	return telegramIDs, nil
}

func (store *QualificationStore) ActiveVPNUserIDs(ctx context.Context) ([]int64, error) {
	if store == nil || store.database == nil {
		return nil, errors.New("qualification database is required")
	}
	row := store.database.QueryRow(ctx, `
		SELECT COALESCE(array_agg(telegram_id ORDER BY telegram_id), ARRAY[]::BIGINT[])
		FROM vpn_users
		WHERE status = 'active' AND eligible = TRUE`)
	var telegramIDs []int64
	if err := row.Scan(&telegramIDs); err != nil {
		return nil, fmt.Errorf("query active VPN users: %w", err)
	}
	previous := int64(0)
	for _, telegramID := range telegramIDs {
		if telegramID <= previous {
			return nil, errors.New("persisted active VPN users are invalid")
		}
		previous = telegramID
	}
	return telegramIDs, nil
}

func (store *QualificationStore) RecheckSettings(ctx context.Context) (qualification.RecheckSettings, error) {
	if store == nil || store.database == nil {
		return qualification.RecheckSettings{}, errors.New("qualification database is required")
	}
	row := store.database.QueryRow(ctx, `
		SELECT recheck_interval_minutes, recheck_requests_per_second, recheck_batch_size
		FROM qualification_settings
		WHERE singleton`)
	var intervalMinutes int
	var requestsPerSecond int
	var batchSize int
	if err := row.Scan(&intervalMinutes, &requestsPerSecond, &batchSize); err != nil {
		return qualification.RecheckSettings{}, fmt.Errorf("query qualification recheck settings: %w", err)
	}
	settings := qualification.RecheckSettings{
		Interval:          time.Duration(intervalMinutes) * time.Minute,
		RequestsPerSecond: requestsPerSecond,
		BatchSize:         batchSize,
	}
	if err := settings.Validate(); err != nil {
		return qualification.RecheckSettings{}, fmt.Errorf("persisted qualification recheck settings are invalid: %w", err)
	}
	return settings, nil
}
