package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/reality"
)

type RealityHealthStore struct {
	transactions TransactionRunner
	database     Database
}

func NewRealityHealthStore(transactions TransactionRunner, database Database) *RealityHealthStore {
	return &RealityHealthStore{transactions: transactions, database: database}
}

func (store *RealityHealthStore) CurrentRealityTarget(ctx context.Context) (string, error) {
	if store == nil || store.database == nil {
		return "", errors.New("REALITY target database is required")
	}
	var target string
	err := store.database.QueryRow(ctx, `
SELECT reality_server
FROM core_settings
WHERE singleton = TRUE
  AND configured = TRUE
  AND reality_server <> ''`).Scan(&target)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", reality.ErrRealityTargetNotConfigured
	}
	if err != nil {
		return "", fmt.Errorf("read configured REALITY target: %w", err)
	}
	if err := reality.ValidateTargetDomain(target); err != nil {
		return "", errors.New("persisted REALITY target is invalid")
	}
	return target, nil
}

func (store *RealityHealthStore) RecordRealityHealth(ctx context.Context, target string, healthy bool, at time.Time) (reality.HealthTransition, error) {
	if store == nil || store.transactions == nil {
		return "", errors.New("REALITY health transaction runner is required")
	}
	if err := reality.ValidateTargetDomain(target); err != nil {
		return "", err
	}
	if at.IsZero() {
		return "", errors.New("REALITY health observation time is required")
	}
	at = at.UTC()
	transition := reality.HealthTransitionNone
	err := store.transactions.RunInTransaction(ctx, func(database Database) error {
		var previousTarget string
		var previousHealthy bool
		var previousPending string
		err := database.QueryRow(ctx, `
SELECT target_domain, healthy, COALESCE(notification_pending, '')
FROM reality_health
WHERE singleton = TRUE
FOR UPDATE`).Scan(&previousTarget, &previousHealthy, &previousPending)
		if errors.Is(err, pgx.ErrNoRows) {
			if !healthy {
				transition = reality.HealthTransitionFailed
			}
			_, err = database.Exec(ctx, `
INSERT INTO reality_health (singleton, target_domain, healthy, last_checked_at, last_transition_at, notification_pending)
VALUES (TRUE, $1, $2, $3, $3, NULLIF($4, ''))`, target, healthy, at, pendingRealityTransition(transition))
			return err
		}
		if err != nil {
			return fmt.Errorf("lock REALITY health state: %w", err)
		}
		if previousTarget != target {
			if !healthy {
				transition = reality.HealthTransitionFailed
			}
		} else if previousHealthy != healthy {
			if healthy {
				transition = reality.HealthTransitionRecovered
			} else {
				transition = reality.HealthTransitionFailed
			}
		} else {
			switch reality.HealthTransition(previousPending) {
			case reality.HealthTransitionFailed, reality.HealthTransitionRecovered:
				transition = reality.HealthTransition(previousPending)
			case "", reality.HealthTransitionNone:
			default:
				return errors.New("persisted REALITY health notification state is invalid")
			}
		}
		_, err = database.Exec(ctx, `
UPDATE reality_health
SET target_domain = $1,
    healthy = $2,
    last_checked_at = $3,
    last_transition_at = CASE
        WHEN target_domain <> $1 OR healthy <> $2 THEN $3
        ELSE last_transition_at
    END,
    notification_pending = CASE
        WHEN target_domain <> $1 OR healthy <> $2 THEN NULLIF($4, '')
        ELSE notification_pending
    END
WHERE singleton = TRUE`, target, healthy, at, pendingRealityTransition(transition))
		return err
	})
	if err != nil {
		return "", fmt.Errorf("persist REALITY health observation: %w", err)
	}
	return transition, nil
}

func pendingRealityTransition(transition reality.HealthTransition) string {
	if transition == reality.HealthTransitionFailed || transition == reality.HealthTransitionRecovered {
		return string(transition)
	}
	return ""
}

func (store *RealityHealthStore) AcknowledgeRealityHealthNotification(ctx context.Context, target string, transition reality.HealthTransition, at time.Time) error {
	if store == nil || store.transactions == nil {
		return errors.New("REALITY health transaction runner is required")
	}
	if err := reality.ValidateTargetDomain(target); err != nil {
		return err
	}
	if transition != reality.HealthTransitionFailed && transition != reality.HealthTransitionRecovered {
		return errors.New("REALITY health notification transition is invalid")
	}
	if at.IsZero() {
		return errors.New("REALITY health notification time is required")
	}
	at = at.UTC()
	return store.transactions.RunInTransaction(ctx, func(database Database) error {
		_, err := database.Exec(ctx, `
UPDATE reality_health
SET notification_pending = NULL,
    last_notification_at = $3
WHERE singleton = TRUE
  AND target_domain = $1
  AND notification_pending = $2`, target, string(transition), at)
		return err
	})
}
