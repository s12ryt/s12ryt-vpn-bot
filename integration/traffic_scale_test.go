//go:build integration

package integration

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/postgres"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/trafficstats"
)

const (
	scaleUserCount = 600
	scaleBatchSize = 50
	scaleFirstID   = int64(9_000_001)
)

func TestTrafficScaleConcurrentIngestion(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	defer pool.Close()

	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire migration connection: %v", err)
	}
	if err := postgres.Migrate(ctx, connection); err != nil {
		connection.Release()
		t.Fatalf("migrate database: %v", err)
	}
	connection.Release()

	periodStartedAt := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	observedAt := periodStartedAt.Add(time.Hour)
	lastID := scaleFirstID + scaleUserCount - 1
	var batches []trafficstats.PendingBatch
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		for _, batch := range batches {
			_, _ = pool.Exec(cleanupCtx, `DELETE FROM traffic_ingestion_batches WHERE batch_id = $1`, batch.ID)
		}
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM vpn_users WHERE telegram_id BETWEEN $1 AND $2`, scaleFirstID, lastID)
	}()
	if _, err := pool.Exec(ctx, `
		INSERT INTO vpn_users (
			telegram_id, eligible, status, credential_generation,
			period_started_at, last_vpn_activity_at, created_at, updated_at
		)
		SELECT id, TRUE, 'active', 1, $1, $1, $1, $1
		FROM generate_series($2::BIGINT, $3::BIGINT) AS id
		ON CONFLICT (telegram_id) DO UPDATE SET
			eligible = EXCLUDED.eligible,
			status = EXCLUDED.status,
			credential_generation = EXCLUDED.credential_generation,
			period_started_at = EXCLUDED.period_started_at,
			last_vpn_activity_at = EXCLUDED.last_vpn_activity_at,
			updated_at = EXCLUDED.updated_at`, periodStartedAt, scaleFirstID, lastID); err != nil {
		t.Fatalf("seed VPN users: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO quota_windows (
			telegram_id, period_started_at, period_seconds,
			limit_bytes, used_bytes, blocked, updated_at
		)
		SELECT id, $1, 2592000, 50000000000, 0, FALSE, $1
		FROM generate_series($2::BIGINT, $3::BIGINT) AS id
		ON CONFLICT (telegram_id) DO UPDATE SET
			period_started_at = EXCLUDED.period_started_at,
			period_seconds = EXCLUDED.period_seconds,
			limit_bytes = EXCLUDED.limit_bytes,
			used_bytes = EXCLUDED.used_bytes,
			blocked = EXCLUDED.blocked,
			updated_at = EXCLUDED.updated_at`, periodStartedAt, scaleFirstID, lastID); err != nil {
		t.Fatalf("seed quota windows: %v", err)
	}

	batches = make([]trafficstats.PendingBatch, 0, scaleUserCount/scaleBatchSize)
	for batchIndex := 0; batchIndex < scaleUserCount/scaleBatchSize; batchIndex++ {
		samples := make([]trafficstats.Sample, 0, scaleBatchSize)
		for offset := 0; offset < scaleBatchSize; offset++ {
			telegramID := scaleFirstID + int64(batchIndex*scaleBatchSize+offset)
			// 8 and 16 represent counters aggregated from four protocols on
			// both IPv4 and IPv6 into the shared per-user quota.
			samples = append(samples, trafficstats.Sample{TelegramID: telegramID, Uplink: 8, Downlink: 16})
		}
		batch, batchErr := trafficstats.NewPendingBatch(observedAt, samples)
		if batchErr != nil {
			t.Fatalf("NewPendingBatch(%d) error = %v", batchIndex, batchErr)
		}
		batches = append(batches, batch)
	}
	store := postgres.NewTrafficStore(postgres.NewTransactionRunner(pool))
	started := time.Now()
	var wait sync.WaitGroup
	results := make(chan postgres.TrafficBatchResult, len(batches))
	errors := make(chan error, len(batches))
	for _, batch := range batches {
		batch := batch
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, recordErr := store.RecordPendingBatch(ctx, batch)
			if recordErr != nil {
				errors <- recordErr
				return
			}
			results <- result
		}()
	}
	wait.Wait()
	close(results)
	close(errors)
	for recordErr := range errors {
		t.Errorf("RecordPendingBatch() error = %v", recordErr)
	}
	if t.Failed() {
		return
	}
	if elapsed := time.Since(started); elapsed > 60*time.Second {
		t.Fatalf("600-user ingestion took %s, want under 60s", elapsed)
	} else {
		t.Logf("ingested 600 users in %s", elapsed)
	}

	applied := 0
	for result := range results {
		applied += result.Applied
		if len(result.RevokedTelegramIDs) != 0 || len(result.RestoredTelegramIDs) != 0 {
			t.Fatalf("unexpected quota transition: %#v", result)
		}
	}
	if applied != scaleUserCount {
		t.Fatalf("applied = %d, want %d", applied, scaleUserCount)
	}

	replayed, err := store.RecordPendingBatch(ctx, batches[0])
	if err != nil {
		t.Fatalf("replay RecordPendingBatch() error = %v", err)
	}
	if replayed.Applied != 0 {
		t.Fatalf("replayed batch applied = %d, want 0", replayed.Applied)
	}

	var quotaCount, correctQuotaCount int
	var usedTotal int64
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE used_bytes = 24 AND blocked = FALSE),
		       COALESCE(SUM(used_bytes), 0)
		FROM quota_windows
		WHERE telegram_id BETWEEN $1 AND $2`, scaleFirstID, lastID).Scan(&quotaCount, &correctQuotaCount, &usedTotal); err != nil {
		t.Fatalf("verify quota totals: %v", err)
	}
	if quotaCount != scaleUserCount || correctQuotaCount != scaleUserCount || usedTotal != scaleUserCount*24 {
		t.Fatalf("quota verification = count %d correct %d total %d, want %d/%d/%d", quotaCount, correctQuotaCount, usedTotal, scaleUserCount, scaleUserCount, scaleUserCount*24)
	}

	var activityCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM vpn_users
		WHERE telegram_id BETWEEN $1 AND $2
		  AND last_vpn_activity_at = $3`, scaleFirstID, lastID, observedAt).Scan(&activityCount); err != nil {
		t.Fatalf("verify activity timestamps: %v", err)
	}
	if activityCount != scaleUserCount {
		t.Fatalf("updated activity rows = %d, want %d", activityCount, scaleUserCount)
	}

	var batchCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM traffic_ingestion_batches
		WHERE collected_at = $1`, observedAt).Scan(&batchCount); err != nil {
		t.Fatalf("verify traffic batches: %v", err)
	}
	if batchCount < len(batches) {
		t.Fatalf("committed batch count = %d, want at least %d", batchCount, len(batches))
	}
}
