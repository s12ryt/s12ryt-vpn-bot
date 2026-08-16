package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

type AuditStore struct {
	database Database
}

func NewAuditStore(database Database) *AuditStore {
	return &AuditStore{database: database}
}

func (store *AuditStore) List(ctx context.Context, before int64, limit int) ([]domain.AuditEvent, error) {
	if store == nil || store.database == nil || before < 0 || limit < 1 || limit > 200 {
		return nil, errors.New("audit list window is invalid")
	}
	var payload []byte
	err := store.database.QueryRow(ctx, `
		SELECT COALESCE(jsonb_agg(jsonb_build_object(
			'id', audit_event.id,
			'actor_telegram_id', audit_event.actor_telegram_id,
			'action', audit_event.action,
			'target_type', audit_event.target_type,
			'target_id', audit_event.target_id,
			'details', audit_event.details,
			'created_at', audit_event.created_at
		) ORDER BY audit_event.id DESC), '[]'::jsonb)
		FROM (
			SELECT id, actor_telegram_id, action, target_type, target_id, details, created_at
			FROM audit_events
			WHERE ($1::bigint = 0 OR id < $1)
			ORDER BY id DESC
			LIMIT $2
		) AS audit_event`, before, limit).Scan(&payload)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	var events []domain.AuditEvent
	if err := json.Unmarshal(payload, &events); err != nil {
		return nil, errors.New("decode audit events")
	}
	var previous int64
	for index, event := range events {
		if event.ID <= 0 || (index > 0 && event.ID >= previous) || event.ActorTelegramID != nil && *event.ActorTelegramID <= 0 ||
			strings.TrimSpace(event.Action) == "" || strings.TrimSpace(event.TargetType) == "" || event.CreatedAt.IsZero() || !jsonObject(event.Details) {
			return nil, errors.New("persisted audit event is invalid")
		}
		previous = event.ID
	}
	return events, nil
}

func jsonObject(value json.RawMessage) bool {
	var object map[string]json.RawMessage
	return len(value) > 0 && json.Unmarshal(value, &object) == nil && object != nil
}

func (store *AuditStore) RecordPlannedRestartNotification(ctx context.Context, attempted, failed int, at time.Time) error {
	if store == nil || store.database == nil || !validNotificationCounts(attempted, failed) || at.IsZero() {
		return errors.New("planned restart notification audit is invalid")
	}
	_, err := store.database.Exec(ctx, `
		INSERT INTO audit_events (action, target_type, details, created_at)
		VALUES ('core.planned_restart_notification', 'core',
		        jsonb_build_object('attempted', $1::integer, 'failed', $2::integer), $3)`,
		attempted, failed, at)
	if err != nil {
		return fmt.Errorf("record planned restart notification audit: %w", err)
	}
	return nil
}

func (store *AuditStore) RecordCoreFailureNotification(ctx context.Context, failure CoreFailureCode, attempted, failed int, at time.Time) error {
	if store == nil || store.database == nil || !validCoreFailure(failure) ||
		!validNotificationCounts(attempted, failed) || at.IsZero() {
		return errors.New("core failure notification audit is invalid")
	}
	_, err := store.database.Exec(ctx, `
		INSERT INTO audit_events (action, target_type, details, created_at)
		VALUES ('core.failure_notification', 'core',
		        jsonb_build_object('failure', $1::text, 'attempted', $2::integer, 'failed', $3::integer), $4)`,
		string(failure), attempted, failed, at)
	if err != nil {
		return fmt.Errorf("record core failure notification audit: %w", err)
	}
	return nil
}

func (store *AuditStore) RecordTrafficFailureNotification(ctx context.Context, stage string, failClosed bool, attempted, failed int, at time.Time) error {
	if store == nil || store.database == nil || !validTrafficFailureStage(stage) ||
		!validNotificationCounts(attempted, failed) || at.IsZero() {
		return errors.New("traffic failure notification audit is invalid")
	}
	_, err := store.database.Exec(ctx, `
		INSERT INTO audit_events (action, target_type, details, created_at)
		VALUES ('traffic.failure_notification', 'traffic',
		        jsonb_build_object('stage', $1::text, 'fail_closed', $2::boolean,
		                           'attempted', $3::integer, 'failed', $4::integer), $5)`,
		stage, failClosed, attempted, failed, at)
	if err != nil {
		return fmt.Errorf("record traffic failure notification audit: %w", err)
	}
	return nil
}

func (store *AuditStore) RecordTrafficRecoveryNotification(ctx context.Context, wasFailClosed bool, attempted, failed int, at time.Time) error {
	if store == nil || store.database == nil || !validNotificationCounts(attempted, failed) || at.IsZero() {
		return errors.New("traffic recovery notification audit is invalid")
	}
	_, err := store.database.Exec(ctx, `
		INSERT INTO audit_events (action, target_type, details, created_at)
		VALUES ('traffic.recovery_notification', 'traffic',
		        jsonb_build_object('was_fail_closed', $1::boolean,
		                           'attempted', $2::integer, 'failed', $3::integer), $4)`,
		wasFailClosed, attempted, failed, at)
	if err != nil {
		return fmt.Errorf("record traffic recovery notification audit: %w", err)
	}
	return nil
}

func validNotificationCounts(attempted, failed int) bool {
	return attempted >= 0 && failed >= 0 && failed <= attempted
}
