package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/qualification"
)

var ErrInactivityConfirmationRequired = errors.New("inactivity removal confirmation is required")

type ManagementSettingsStore struct {
	transactions TransactionRunner
	database     Database
	trigger      qualification.RecheckTrigger
}

func NewManagementSettingsStore(transactions TransactionRunner, database Database, trigger qualification.RecheckTrigger) *ManagementSettingsStore {
	return &ManagementSettingsStore{transactions: transactions, database: database, trigger: trigger}
}

func (store *ManagementSettingsStore) Get(ctx context.Context) (domain.ManagementSettings, []domain.QualificationRuleOverview, error) {
	if store == nil || store.database == nil {
		return domain.ManagementSettings{}, nil, errors.New("management settings database is required")
	}
	row := store.database.QueryRow(ctx, `
		SELECT jsonb_build_object(
			'settings', jsonb_build_object(
				'qualification_mode', setting.mode,
				'recheck_interval_minutes', setting.recheck_interval_minutes,
				'recheck_requests_per_second', setting.recheck_requests_per_second,
				'recheck_batch_size', setting.recheck_batch_size,
				'inactivity_threshold_days', setting.inactivity_threshold_days,
				'quota_limit_bytes', setting.quota_limit_bytes
			),
			'rules', COALESCE((
				SELECT jsonb_agg(jsonb_build_object(
					'chat_id', rule.chat_id,
					'chat_type', rule.chat_type,
					'title', rule.title,
					'enabled', rule.enabled,
					'bot_administrator_passed', rule.bot_admin_verified_at IS NOT NULL
				) ORDER BY rule.chat_id)
				FROM qualification_rules AS rule
			), '[]'::jsonb)
		)
		FROM qualification_settings AS setting
		WHERE setting.singleton`)
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		return domain.ManagementSettings{}, nil, fmt.Errorf("query management settings: %w", err)
	}
	var payload struct {
		Settings domain.ManagementSettings          `json:"settings"`
		Rules    []domain.QualificationRuleOverview `json:"rules"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return domain.ManagementSettings{}, nil, errors.New("persisted management settings are malformed")
	}
	if err := payload.Settings.Validate(); err != nil {
		return domain.ManagementSettings{}, nil, fmt.Errorf("persisted management settings are invalid: %w", err)
	}
	previous := int64(math.MinInt64)
	for _, rule := range payload.Rules {
		if err := rule.Validate(); err != nil || rule.ChatID <= previous {
			return domain.ManagementSettings{}, nil, errors.New("persisted qualification rules are invalid")
		}
		previous = rule.ChatID
	}
	return payload.Settings, payload.Rules, nil
}

func (store *ManagementSettingsStore) PreviewInactivity(ctx context.Context, thresholdDays int, now time.Time) (int64, error) {
	if store == nil || store.database == nil {
		return 0, errors.New("management settings database is required")
	}
	if thresholdDays < 1 || now.IsZero() {
		return 0, errors.New("inactivity preview input is invalid")
	}
	var count int64
	if err := store.database.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM vpn_users
		WHERE status = 'active'
		  AND last_vpn_activity_at <= $1 - make_interval(days => $2)`, now, thresholdDays).Scan(&count); err != nil {
		return 0, fmt.Errorf("preview inactive VPN users: %w", err)
	}
	if count < 0 {
		return 0, errors.New("persisted inactivity preview count is invalid")
	}
	return count, nil
}

func (store *ManagementSettingsStore) Update(ctx context.Context, actorTelegramID int64, settings domain.ManagementSettings, confirmInactivityRemoval bool, now time.Time) error {
	if store == nil || store.transactions == nil || store.trigger == nil {
		return errors.New("management settings dependencies are required")
	}
	if actorTelegramID <= 0 || now.IsZero() {
		return errors.New("management settings update metadata is invalid")
	}
	if err := settings.Validate(); err != nil {
		return err
	}
	err := store.transactions.RunInTransaction(ctx, func(transaction Database) error {
		var previousInactivityDays int
		if err := transaction.QueryRow(ctx, `
			SELECT inactivity_threshold_days
			FROM qualification_settings
			WHERE singleton
			FOR UPDATE`).Scan(&previousInactivityDays); err != nil {
			return fmt.Errorf("lock management settings: %w", err)
		}
		removesInactiveUsers := settings.InactivityThresholdDays > 0 &&
			(previousInactivityDays == 0 || settings.InactivityThresholdDays < previousInactivityDays)
		if removesInactiveUsers && !confirmInactivityRemoval {
			return ErrInactivityConfirmationRequired
		}
		if _, err := transaction.Exec(ctx, `
			UPDATE qualification_settings
			SET mode = $1,
			    recheck_interval_minutes = $2,
			    recheck_requests_per_second = $3,
			    recheck_batch_size = $4,
			    inactivity_threshold_days = $5,
			    quota_limit_bytes = $6,
			    updated_at = $7
			WHERE singleton`,
			string(settings.QualificationMode), settings.RecheckIntervalMinutes,
			settings.RecheckRequestsPerSecond, settings.RecheckBatchSize,
			settings.InactivityThresholdDays, settings.QuotaLimitBytes, now); err != nil {
			return fmt.Errorf("update management settings: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
			WITH previous AS MATERIALIZED (
				SELECT telegram_id, blocked
				FROM quota_windows
				FOR UPDATE
			), updated AS (
				UPDATE quota_windows AS quota
				SET limit_bytes = $1,
				    blocked = quota.used_bytes >= $1,
				    updated_at = $2
				FROM previous
				WHERE quota.telegram_id = previous.telegram_id
				RETURNING quota.telegram_id, previous.blocked AS was_blocked, quota.blocked
			), actions AS (
				SELECT updated.telegram_id,
				       CASE WHEN updated.blocked THEN 'revoke' ELSE 'reconcile' END AS action
				FROM updated
				JOIN vpn_users AS vpn_user USING (telegram_id)
				CROSS JOIN traffic_health AS health
				WHERE updated.was_blocked <> updated.blocked
				  AND vpn_user.status = 'active'
				  AND vpn_user.eligible = TRUE
				  AND (updated.blocked OR health.fail_closed = FALSE)
			)
			INSERT INTO core_action_outbox (telegram_id, action, available_at)
			SELECT telegram_id, action, $2 FROM actions
			ON CONFLICT (telegram_id, action) WHERE completed_at IS NULL DO NOTHING`, settings.QuotaLimitBytes, now); err != nil {
			return fmt.Errorf("apply quota setting: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
			WITH removed AS (
				UPDATE vpn_users
				SET status = 'self_service', updated_at = $2
				WHERE $3
				  AND status = 'active'
				  AND last_vpn_activity_at <= $2 - make_interval(days => $1)
				RETURNING telegram_id
			)
			INSERT INTO core_action_outbox (telegram_id, action, available_at)
			SELECT telegram_id, 'revoke', $2 FROM removed
			ON CONFLICT (telegram_id, action) WHERE completed_at IS NULL DO NOTHING`, settings.InactivityThresholdDays, now, removesInactiveUsers); err != nil {
			return fmt.Errorf("apply inactivity setting: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO audit_events (actor_telegram_id, action, target_type, target_id, details, created_at)
			VALUES ($1, 'settings.management.update', 'settings', 'management',
			        jsonb_build_object('qualification_mode', $2, 'recheck_interval_minutes', $3,
			                           'recheck_requests_per_second', $4, 'recheck_batch_size', $5,
			                           'inactivity_threshold_days', $6, 'quota_limit_bytes', $7), $8)`,
			actorTelegramID, string(settings.QualificationMode), settings.RecheckIntervalMinutes,
			settings.RecheckRequestsPerSecond, settings.RecheckBatchSize,
			settings.InactivityThresholdDays, settings.QuotaLimitBytes, now); err != nil {
			return fmt.Errorf("audit management settings update: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	store.trigger.Trigger()
	return nil
}
