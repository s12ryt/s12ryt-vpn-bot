package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/trafficstats"
)

type TrafficBatchResult struct {
	Applied             int
	RevokedTelegramIDs  []int64
	RestoredTelegramIDs []int64
}

type TrafficFaultObservation struct {
	StartedAt  time.Time
	FailClosed bool
	Notify     bool
}

type TrafficFaultRecovery struct {
	Recovered     bool
	WasFailClosed bool
	StartedAt     time.Time
}

type TrafficStore struct {
	transactions TransactionRunner
}

func NewTrafficStore(transactions TransactionRunner) *TrafficStore {
	return &TrafficStore{transactions: transactions}
}

func (store *TrafficStore) AdvanceDueQuotaPeriods(ctx context.Context, now time.Time, limit int) (int64, int64, error) {
	if store == nil || store.transactions == nil || now.IsZero() || limit < 1 || limit > 1000 {
		return 0, 0, errors.New("quota period sweep is invalid")
	}
	var advanced int64
	var restored int64
	err := store.transactions.RunInTransaction(ctx, func(transaction Database) error {
		if transaction == nil {
			return errors.New("traffic transaction is required")
		}
		if err := transaction.QueryRow(ctx, `
			WITH due AS (
				SELECT quota.telegram_id, quota.blocked, quota.period_started_at, quota.period_seconds
				FROM quota_windows AS quota
				WHERE quota.period_started_at + quota.period_seconds * INTERVAL '1 second' <= $1
				ORDER BY quota.period_started_at, quota.telegram_id
				LIMIT $2
				FOR UPDATE OF quota SKIP LOCKED
			), updated AS (
				UPDATE quota_windows AS quota
				SET period_started_at = due.period_started_at +
					(FLOOR(EXTRACT(EPOCH FROM ($1 - due.period_started_at)) / due.period_seconds)::BIGINT * due.period_seconds) * INTERVAL '1 second',
				    used_bytes = 0,
				    blocked = FALSE,
				    updated_at = $1
				FROM due
				WHERE quota.telegram_id = due.telegram_id
				RETURNING quota.telegram_id, due.blocked AS was_blocked
			), queued AS (
				INSERT INTO core_action_outbox (telegram_id, action)
				SELECT updated.telegram_id, 'reconcile'
				FROM updated
				JOIN vpn_users AS vpn_user ON vpn_user.telegram_id = updated.telegram_id
				CROSS JOIN traffic_health
				WHERE updated.was_blocked = TRUE
				  AND vpn_user.status = 'active'
				  AND vpn_user.eligible = TRUE
				  AND traffic_health.singleton = TRUE
				  AND traffic_health.fail_closed = FALSE
				ON CONFLICT (telegram_id, action) WHERE completed_at IS NULL DO NOTHING
				RETURNING telegram_id
			)
			SELECT (SELECT COUNT(*) FROM updated), (SELECT COUNT(*) FROM queued)`, now, limit).Scan(&advanced, &restored); err != nil {
			return fmt.Errorf("advance due quota periods: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return advanced, restored, nil
}

func (store *TrafficStore) ObserveFailure(ctx context.Context, stage string, now time.Time) (TrafficFaultObservation, error) {
	if store == nil || store.transactions == nil || now.IsZero() || !validTrafficFailureStage(stage) {
		return TrafficFaultObservation{}, errors.New("traffic failure observation is invalid")
	}
	var observation TrafficFaultObservation
	err := store.transactions.RunInTransaction(ctx, func(transaction Database) error {
		if transaction == nil {
			return errors.New("traffic transaction is required")
		}
		var transitioned bool
		if err := transaction.QueryRow(ctx, `
			WITH previous AS (
				SELECT fail_closed
				FROM traffic_health
				WHERE singleton = TRUE
				FOR UPDATE
			), updated AS (
				UPDATE traffic_health
				SET failure_started_at = COALESCE(failure_started_at, $2),
				    failure_stage = $1,
				    fail_closed = fail_closed OR COALESCE(failure_started_at, $2) <= $2 - INTERVAL '5 minutes',
				    last_notified_at = CASE
				        WHEN last_notified_at IS NULL OR last_notified_at <= $2 - INTERVAL '15 minutes' THEN $2
				        ELSE last_notified_at
				    END,
				    updated_at = $2
				WHERE singleton = TRUE
				RETURNING failure_started_at, fail_closed, last_notified_at = $2 AS notify
			)
			SELECT updated.failure_started_at, updated.fail_closed, updated.notify,
			       NOT previous.fail_closed AND updated.fail_closed
			FROM updated, previous`, stage, now).Scan(
			&observation.StartedAt, &observation.FailClosed, &observation.Notify, &transitioned,
		); err != nil {
			return fmt.Errorf("observe traffic failure: %w", err)
		}
		if transitioned {
			return queueTrafficHealthActions(ctx, transaction, "revoke")
		}
		return nil
	})
	if err != nil {
		return TrafficFaultObservation{}, err
	}
	return observation, nil
}

func (store *TrafficStore) ObserveRecovery(ctx context.Context, now time.Time) (TrafficFaultRecovery, error) {
	if store == nil || store.transactions == nil || now.IsZero() {
		return TrafficFaultRecovery{}, errors.New("traffic recovery observation is invalid")
	}
	var recovery TrafficFaultRecovery
	err := store.transactions.RunInTransaction(ctx, func(transaction Database) error {
		if transaction == nil {
			return errors.New("traffic transaction is required")
		}
		err := transaction.QueryRow(ctx, `
			WITH previous AS (
				SELECT failure_started_at, fail_closed
				FROM traffic_health
				WHERE singleton = TRUE AND failure_started_at IS NOT NULL
				FOR UPDATE
			), updated AS (
				UPDATE traffic_health
				SET fail_closed = FALSE, failure_started_at = NULL, failure_stage = NULL,
				    last_notified_at = NULL, updated_at = $1
				WHERE singleton = TRUE AND EXISTS (SELECT 1 FROM previous)
				RETURNING TRUE
			)
			SELECT previous.failure_started_at, previous.fail_closed
			FROM previous, updated`, now).Scan(&recovery.StartedAt, &recovery.WasFailClosed)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("observe traffic recovery: %w", err)
		}
		recovery.Recovered = true
		if recovery.WasFailClosed {
			return queueTrafficHealthActions(ctx, transaction, "reconcile")
		}
		return nil
	})
	if err != nil {
		return TrafficFaultRecovery{}, err
	}
	return recovery, nil
}

func validTrafficFailureStage(stage string) bool {
	switch stage {
	case "collect", "spool", "record", "cleanup":
		return true
	default:
		return false
	}
}

func queueTrafficHealthActions(ctx context.Context, transaction Database, action string) error {
	if _, err := transaction.Exec(ctx, `
		INSERT INTO core_action_outbox (telegram_id, action)
		SELECT vpn_user.telegram_id, $1
		FROM vpn_users AS vpn_user
		JOIN quota_windows AS quota ON quota.telegram_id = vpn_user.telegram_id
		WHERE vpn_user.status = 'active'
		  AND vpn_user.eligible = TRUE
		  AND ($1 = 'revoke' OR quota.blocked = FALSE)
		ON CONFLICT (telegram_id, action) WHERE completed_at IS NULL DO NOTHING`, action); err != nil {
		return fmt.Errorf("queue traffic health core actions: %w", err)
	}
	return nil
}

func (store *TrafficStore) SetFailClosed(ctx context.Context, failClosed bool, now time.Time) (bool, error) {
	if store == nil || store.transactions == nil {
		return false, errors.New("traffic transaction runner is required")
	}
	if now.IsZero() {
		return false, errors.New("traffic health transition time is required")
	}

	changed := false
	err := store.transactions.RunInTransaction(ctx, func(transaction Database) error {
		if transaction == nil {
			return errors.New("traffic transaction is required")
		}
		var transitioned bool
		err := transaction.QueryRow(ctx, `
			UPDATE traffic_health
			SET fail_closed = $1,
			    failure_started_at = CASE WHEN $1 THEN $2 ELSE NULL END,
			    failure_stage = CASE WHEN $1 THEN 'record' ELSE NULL END,
			    last_notified_at = CASE WHEN $1 THEN $2 ELSE NULL END,
			    updated_at = $2
			WHERE singleton = TRUE
			  AND fail_closed <> $1
			RETURNING TRUE`, failClosed, now).Scan(&transitioned)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("transition traffic health: %w", err)
		}
		if !transitioned {
			return nil
		}
		action := "reconcile"
		if failClosed {
			action = "revoke"
		}
		if err := queueTrafficHealthActions(ctx, transaction, action); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}

func (store *TrafficStore) RecordBatch(
	ctx context.Context,
	observedAt time.Time,
	samples []trafficstats.Sample,
) (TrafficBatchResult, error) {
	return store.recordBatch(ctx, observedAt, samples, "")
}

func (store *TrafficStore) RecordPendingBatch(ctx context.Context, batch trafficstats.PendingBatch) (TrafficBatchResult, error) {
	validated, err := trafficstats.NewPendingBatch(batch.CollectedAt, batch.Samples)
	if err != nil || validated.ID != batch.ID {
		return TrafficBatchResult{}, errors.New("pending traffic batch is invalid")
	}
	return store.recordBatch(ctx, batch.CollectedAt, batch.Samples, batch.ID)
}

func (store *TrafficStore) recordBatch(
	ctx context.Context,
	observedAt time.Time,
	samples []trafficstats.Sample,
	batchID string,
) (TrafficBatchResult, error) {
	if store == nil || store.transactions == nil {
		return TrafficBatchResult{}, errors.New("traffic transaction runner is required")
	}
	if observedAt.IsZero() {
		return TrafficBatchResult{}, errors.New("traffic observation timestamp is required")
	}
	positive := make([]trafficstats.Sample, 0, len(samples))
	var previousID int64
	for index, sample := range samples {
		if sample.TelegramID <= 0 || sample.Uplink < 0 || sample.Downlink < 0 || sample.Uplink > math.MaxInt64-sample.Downlink {
			return TrafficBatchResult{}, errors.New("traffic sample is invalid")
		}
		if index > 0 && sample.TelegramID <= previousID {
			return TrafficBatchResult{}, errors.New("traffic samples must be strictly ordered")
		}
		previousID = sample.TelegramID
		if sample.Uplink+sample.Downlink > 0 {
			positive = append(positive, sample)
		}
	}
	if len(positive) == 0 {
		return TrafficBatchResult{}, nil
	}

	var pending TrafficBatchResult
	err := store.transactions.RunInTransaction(ctx, func(transaction Database) error {
		if transaction == nil {
			return errors.New("traffic transaction is required")
		}
		if batchID != "" {
			var inserted bool
			if err := transaction.QueryRow(ctx, `
				WITH inserted AS (
					INSERT INTO traffic_ingestion_batches (batch_id, collected_at)
					VALUES ($1, $2)
					ON CONFLICT (batch_id) DO NOTHING
					RETURNING batch_id
				)
				SELECT EXISTS (SELECT 1 FROM inserted)`, batchID, observedAt).Scan(&inserted); err != nil {
				return fmt.Errorf("record traffic batch identity: %w", err)
			}
			if !inserted {
				return nil
			}
		}
		for _, sample := range positive {
			account, quota, persistedBlocked, err := lockTrafficAccount(ctx, transaction, sample.TelegramID)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			before, err := quota.Advance(account.Snapshot().PeriodStartedAt)
			if err != nil {
				return fmt.Errorf("inspect persisted quota: %w", err)
			}
			if before.Blocked != persistedBlocked {
				return errors.New("persisted quota blocked state is inconsistent")
			}
			change, err := domain.RecordAggregatedAccountTraffic(account, quota, observedAt, sample.Uplink, sample.Downlink)
			if err != nil {
				return fmt.Errorf("record traffic for Telegram ID %d: %w", sample.TelegramID, err)
			}
			accountSnapshot := account.Snapshot()
			if _, err := transaction.Exec(ctx, `
				UPDATE vpn_users
				SET last_vpn_activity_at = $1, updated_at = NOW()
				WHERE telegram_id = $2`, accountSnapshot.LastVPNActivityAt, sample.TelegramID); err != nil {
				return fmt.Errorf("persist VPN activity: %w", err)
			}
			if _, err := transaction.Exec(ctx, `
				UPDATE quota_windows
				SET period_started_at = $1, used_bytes = $2, blocked = $3, updated_at = NOW()
				WHERE telegram_id = $4`,
				change.Quota.PeriodStartedAt,
				change.Quota.UsedBytes,
				change.Quota.Blocked,
				sample.TelegramID,
			); err != nil {
				return fmt.Errorf("persist quota usage: %w", err)
			}
			action := ""
			if change.RevokeCredentialsImmediately {
				action = "revoke"
				pending.RevokedTelegramIDs = append(pending.RevokedTelegramIDs, sample.TelegramID)
			} else if change.RestoreCredentialsImmediately {
				action = "reconcile"
				pending.RestoredTelegramIDs = append(pending.RestoredTelegramIDs, sample.TelegramID)
			}
			if action != "" {
				if _, err := transaction.Exec(ctx, `
					INSERT INTO core_action_outbox (telegram_id, action)
					VALUES ($1, $2)
					ON CONFLICT (telegram_id, action) WHERE completed_at IS NULL DO NOTHING`, sample.TelegramID, action); err != nil {
					return fmt.Errorf("queue quota core action: %w", err)
				}
			}
			pending.Applied++
		}
		return nil
	})
	if err != nil {
		return TrafficBatchResult{}, err
	}
	return pending, nil
}

func lockTrafficAccount(
	ctx context.Context,
	transaction Database,
	telegramID int64,
) (*domain.AccessAccount, *domain.QuotaWindow, bool, error) {
	row := transaction.QueryRow(ctx, `
		SELECT vpn_user.telegram_id, vpn_user.eligible, vpn_user.status,
		       vpn_user.credential_generation, vpn_user.period_started_at,
		       vpn_user.last_vpn_activity_at, quota.period_started_at,
		       quota.period_seconds, quota.limit_bytes, quota.used_bytes, quota.blocked
		FROM vpn_users AS vpn_user
		JOIN quota_windows AS quota ON quota.telegram_id = vpn_user.telegram_id
		WHERE vpn_user.telegram_id = $1
		  AND vpn_user.status = 'active'
		  AND vpn_user.eligible = TRUE
		FOR UPDATE OF vpn_user, quota`, telegramID)
	var access domain.AccessSnapshot
	var status string
	var generation int64
	var accountPeriod, lastActivity sql.NullTime
	var quotaPeriod time.Time
	var periodSeconds, limitBytes, usedBytes int64
	var blocked bool
	if err := row.Scan(
		&access.TelegramID,
		&access.Eligible,
		&status,
		&generation,
		&accountPeriod,
		&lastActivity,
		&quotaPeriod,
		&periodSeconds,
		&limitBytes,
		&usedBytes,
		&blocked,
	); err != nil {
		return nil, nil, false, err
	}
	if generation <= 0 || !accountPeriod.Valid || !lastActivity.Valid || periodSeconds <= 0 || periodSeconds > int64(math.MaxInt64/time.Second) {
		return nil, nil, false, errors.New("persisted traffic state is invalid")
	}
	if !accountPeriod.Time.Equal(quotaPeriod) {
		return nil, nil, false, errors.New("persisted access and quota periods differ")
	}
	access.Status = domain.AccessStatus(status)
	access.CredentialGeneration = uint64(generation)
	access.PeriodStartedAt = accountPeriod.Time
	access.LastVPNActivityAt = lastActivity.Time
	account, err := domain.RestoreAccessAccount(access)
	if err != nil {
		return nil, nil, false, fmt.Errorf("restore traffic account: %w", err)
	}
	quota, err := domain.RestoreQuotaWindow(quotaPeriod, limitBytes, time.Duration(periodSeconds)*time.Second, usedBytes)
	if err != nil {
		return nil, nil, false, fmt.Errorf("restore quota window: %w", err)
	}
	return account, quota, blocked, nil
}
