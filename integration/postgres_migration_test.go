//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/postgres"
)

func TestMigrationsApplyAndRemainIdempotent(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	defer connection.Release()
	if err := postgres.Migrate(ctx, connection); err != nil {
		t.Fatalf("first migration run: %v", err)
	}
	if err := postgres.Migrate(ctx, connection); err != nil {
		t.Fatalf("idempotent migration run: %v", err)
	}

	var count int
	if err := connection.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count < 12 {
		t.Fatalf("schema_migrations count = %d, want at least 12", count)
	}
}
