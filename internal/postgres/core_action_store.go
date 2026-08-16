package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type CoreActionType string

const (
	CoreActionRevoke    CoreActionType = "revoke"
	CoreActionReconcile CoreActionType = "reconcile"
)

type CoreFailureCode string

const (
	CoreFailureSnapshot CoreFailureCode = "snapshot"
	CoreFailureCheck    CoreFailureCode = "check"
	CoreFailurePromote  CoreFailureCode = "promote"
	CoreFailureRestart  CoreFailureCode = "restart"
)

type CoreAction struct {
	ID         int64
	TelegramID int64
	Action     CoreActionType
	Attempts   int
}

type CoreActionStore struct {
	database Database
}

func NewCoreActionStore(database Database) *CoreActionStore {
	return &CoreActionStore{database: database}
}

func (store *CoreActionStore) ClaimDue(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]CoreAction, error) {
	if store == nil || store.database == nil {
		return nil, errors.New("core action store database is required")
	}
	if now.IsZero() || lease <= 0 || limit < 1 || limit > 1000 {
		return nil, errors.New("core action claim parameters are invalid")
	}
	var ids, telegramIDs []int64
	var actions []string
	var attempts []int
	err := store.database.QueryRow(ctx, `
		WITH candidates AS (
			SELECT id
			FROM core_action_outbox
			WHERE completed_at IS NULL
			  AND available_at <= $1
			ORDER BY CASE WHEN action = 'revoke' THEN 0 ELSE 1 END, available_at, id
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		), claimed AS (
			UPDATE core_action_outbox AS outbox
			SET attempts = outbox.attempts + 1,
			    available_at = $2
			FROM candidates
			WHERE outbox.id = candidates.id
			RETURNING outbox.id, outbox.telegram_id, outbox.action, outbox.attempts
		)
		SELECT COALESCE(array_agg(id ORDER BY id), ARRAY[]::BIGINT[]),
		       COALESCE(array_agg(telegram_id ORDER BY id), ARRAY[]::BIGINT[]),
		       COALESCE(array_agg(action ORDER BY id), ARRAY[]::TEXT[]),
		       COALESCE(array_agg(attempts ORDER BY id), ARRAY[]::INTEGER[])
		FROM claimed`, now, now.Add(lease), limit).Scan(&ids, &telegramIDs, &actions, &attempts)
	if err != nil {
		return nil, fmt.Errorf("claim core actions: %w", err)
	}
	if len(ids) != len(telegramIDs) || len(ids) != len(actions) || len(ids) != len(attempts) {
		return nil, errors.New("claimed core actions are inconsistent")
	}
	claimed := make([]CoreAction, len(ids))
	for index, id := range ids {
		action := CoreActionType(actions[index])
		if id <= 0 || telegramIDs[index] <= 0 || attempts[index] <= 0 ||
			(index > 0 && ids[index-1] >= id) || !validCoreAction(action) {
			return nil, errors.New("claimed core action is invalid")
		}
		claimed[index] = CoreAction{ID: id, TelegramID: telegramIDs[index], Action: action, Attempts: attempts[index]}
	}
	return claimed, nil
}

func (store *CoreActionStore) Complete(ctx context.Context, ids []int64, now time.Time) error {
	if store == nil || store.database == nil || now.IsZero() || !validUniqueIDs(ids) {
		return errors.New("core action completion parameters are invalid")
	}
	_, err := store.database.Exec(ctx, `
		UPDATE core_action_outbox
		SET completed_at = $2, last_error = ''
		WHERE id = ANY($1) AND completed_at IS NULL`, ids, now)
	if err != nil {
		return fmt.Errorf("complete core actions: %w", err)
	}
	return nil
}

func (store *CoreActionStore) Retry(ctx context.Context, ids []int64, retryAt time.Time, failure CoreFailureCode) error {
	if store == nil || store.database == nil || retryAt.IsZero() || !validUniqueIDs(ids) || !validCoreFailure(failure) {
		return errors.New("core action retry parameters are invalid")
	}
	_, err := store.database.Exec(ctx, `
		UPDATE core_action_outbox
		SET available_at = $2, last_error = $3
		WHERE id = ANY($1) AND completed_at IS NULL`, ids, retryAt, string(failure))
	if err != nil {
		return fmt.Errorf("retry core actions: %w", err)
	}
	return nil
}

func validCoreAction(action CoreActionType) bool {
	return action == CoreActionRevoke || action == CoreActionReconcile
}

func validCoreFailure(failure CoreFailureCode) bool {
	switch failure {
	case CoreFailureSnapshot, CoreFailureCheck, CoreFailurePromote, CoreFailureRestart:
		return true
	default:
		return false
	}
}

func validUniqueIDs(ids []int64) bool {
	if len(ids) == 0 {
		return false
	}
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return false
		}
		if _, exists := seen[id]; exists {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}
