package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMigrateAppliesUnseenMigrationAndRecordsVersion(t *testing.T) {
	database := &migrationDatabaseStub{applied: false}

	if err := Migrate(context.Background(), database); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	joined := strings.Join(database.execSQL, "\n")
	for _, required := range []string{
		"pg_advisory_lock",
		"CREATE TABLE IF NOT EXISTS schema_migrations",
		"CREATE TABLE IF NOT EXISTS administrators",
		"CREATE TABLE IF NOT EXISTS admin_login_codes",
		"CREATE TABLE IF NOT EXISTS admin_sessions",
		"CREATE TABLE IF NOT EXISTS qualification_settings",
		"CREATE TABLE IF NOT EXISTS qualification_rules",
		"CREATE TABLE IF NOT EXISTS vpn_users",
		"CREATE TABLE IF NOT EXISTS credential_bundles",
		"CREATE TABLE IF NOT EXISTS quota_windows",
		"CREATE TABLE IF NOT EXISTS audit_events",
		"CREATE TABLE IF NOT EXISTS core_action_outbox",
		"recheck_requests_per_second",
		"recheck_batch_size",
		"action IN ('revoke', 'reconcile')",
		"CREATE TABLE IF NOT EXISTS core_settings",
		"reality_private_key_ciphertext",
		"CREATE TABLE IF NOT EXISTS traffic_ingestion_batches",
		"CREATE TABLE IF NOT EXISTS traffic_health",
		"INSERT INTO schema_migrations",
		"pg_advisory_unlock",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("executed SQL does not contain %q:\n%s", required, joined)
		}
	}
}

func TestMigrateSkipsAppliedMigration(t *testing.T) {
	database := &migrationDatabaseStub{applied: true}

	if err := Migrate(context.Background(), database); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	joined := strings.Join(database.execSQL, "\n")
	if strings.Contains(joined, "CREATE TABLE IF NOT EXISTS administrators") || strings.Contains(joined, "INSERT INTO schema_migrations") {
		t.Fatalf("already applied migration ran again:\n%s", joined)
	}
}

func TestMigrateDoesNotRecordVersionWhenMigrationFails(t *testing.T) {
	database := &migrationDatabaseStub{migrationErr: errors.New("migration failed")}

	if err := Migrate(context.Background(), database); !errors.Is(err, database.migrationErr) {
		t.Fatalf("Migrate() error = %v, want migration failure", err)
	}
	joined := strings.Join(database.execSQL, "\n")
	if strings.Contains(joined, "INSERT INTO schema_migrations") {
		t.Fatalf("failed migration was recorded:\n%s", joined)
	}
}

type migrationDatabaseStub struct {
	applied      bool
	migrationErr error
	execSQL      []string
}

func (stub *migrationDatabaseStub) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	stub.execSQL = append(stub.execSQL, sql)
	if strings.Contains(sql, "CREATE TABLE IF NOT EXISTS administrators") && stub.migrationErr != nil {
		return pgconn.CommandTag{}, stub.migrationErr
	}
	return pgconn.CommandTag{}, nil
}

func (stub *migrationDatabaseStub) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return &boolRow{value: stub.applied}
}

type boolRow struct {
	value bool
}

func (row *boolRow) Scan(destinations ...any) error {
	if len(destinations) != 1 {
		return errors.New("unexpected destination count")
	}
	destination, ok := destinations[0].(*bool)
	if !ok {
		return errors.New("unexpected destination type")
	}
	*destination = row.value
	return nil
}
