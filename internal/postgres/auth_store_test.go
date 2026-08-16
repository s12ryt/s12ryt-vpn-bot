package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
)

func TestAuthStoreReplacesAndConsumesLoginCode(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	administrator := auth.Administrator{TelegramID: 12345, Role: auth.RoleOwner, Root: true, Active: true}
	record := auth.LoginCodeRecord{TelegramID: 12345, Administrator: administrator, Digest: [32]byte{1, 2, 3}, ExpiresAt: now.Add(5 * time.Minute)}
	database := &databaseStub{row: &rowStub{values: []any{int64(12345), string(auth.RoleOwner), true, true}}}
	store := NewAuthStore(database)

	if err := store.Replace(context.Background(), record); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	if !strings.Contains(database.execSQL, "ON CONFLICT (telegram_id)") || len(database.execArgs) != 3 {
		t.Fatalf("Replace() query = %q args=%#v", database.execSQL, database.execArgs)
	}
	got, err := store.Consume(context.Background(), 12345, record.Digest, now)
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if got != administrator {
		t.Fatalf("Consume() = %#v, want %#v", got, administrator)
	}
	if !strings.Contains(database.querySQL, "DELETE FROM admin_login_codes") || !strings.Contains(database.querySQL, "code.telegram_id =") || !strings.Contains(database.querySQL, "expires_at >") {
		t.Fatalf("Consume() query does not atomically delete an unexpired code: %q", database.querySQL)
	}
}

func TestAuthStoreNormalizesMissingLoginCode(t *testing.T) {
	store := NewAuthStore(&databaseStub{row: &rowStub{err: pgx.ErrNoRows}})
	if _, err := store.Consume(context.Background(), 12345, [32]byte{1}, time.Now()); !errors.Is(err, auth.ErrLoginCodeInvalid) {
		t.Fatalf("Consume() error = %v, want %v", err, auth.ErrLoginCodeInvalid)
	}
}

func TestAuthStoreCreatesAndAtomicallyTouchesSession(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	administrator := auth.Administrator{TelegramID: 12345, Role: auth.RoleAdministrator, Active: true}
	record := auth.SessionRecord{
		Digest:            [32]byte{4, 5, 6},
		Administrator:     administrator,
		CreatedAt:         now,
		LastSeenAt:        now,
		AbsoluteExpiresAt: now.Add(7 * 24 * time.Hour),
	}
	database := &databaseStub{row: &rowStub{values: []any{int64(12345), string(auth.RoleAdministrator), false, true}}}
	store := NewAuthStore(database)

	if err := store.Create(context.Background(), record); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !strings.Contains(database.execSQL, "INSERT INTO admin_sessions") || len(database.execArgs) != 5 {
		t.Fatalf("Create() query = %q args=%#v", database.execSQL, database.execArgs)
	}
	got, err := store.AuthenticateAndTouch(context.Background(), record.Digest, now.Add(time.Hour), 12*time.Hour)
	if err != nil {
		t.Fatalf("AuthenticateAndTouch() error = %v", err)
	}
	if got != administrator {
		t.Fatalf("AuthenticateAndTouch() = %#v, want %#v", got, administrator)
	}
	if !strings.Contains(database.querySQL, "UPDATE admin_sessions") || !strings.Contains(database.querySQL, "last_seen_at >") || !strings.Contains(database.querySQL, "absolute_expires_at >") {
		t.Fatalf("AuthenticateAndTouch() is not an atomic expiry check and touch: %q", database.querySQL)
	}
}

func TestAuthStoreNormalizesMissingSession(t *testing.T) {
	store := NewAuthStore(&databaseStub{row: &rowStub{err: pgx.ErrNoRows}})
	if _, err := store.AuthenticateAndTouch(context.Background(), [32]byte{1}, time.Now(), 12*time.Hour); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Fatalf("AuthenticateAndTouch() error = %v, want %v", err, auth.ErrSessionInvalid)
	}
}

func TestAuthStoreDeletesOneOrAllAdministratorSessions(t *testing.T) {
	database := &databaseStub{}
	store := NewAuthStore(database)

	if err := store.Delete(context.Background(), [32]byte{1, 2, 3}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !strings.Contains(database.execSQL, "DELETE FROM admin_sessions") || len(database.execArgs) != 1 {
		t.Fatalf("Delete() query = %q args=%#v", database.execSQL, database.execArgs)
	}
	if err := store.DeleteAll(context.Background(), 12345); err != nil {
		t.Fatalf("DeleteAll() error = %v", err)
	}
	if !strings.Contains(database.execSQL, "telegram_id = $1") || database.execArgs[0] != int64(12345) {
		t.Fatalf("DeleteAll() query = %q args=%#v", database.execSQL, database.execArgs)
	}
}

func TestAuthStoreFindsOnlyActiveAdministrator(t *testing.T) {
	want := auth.Administrator{TelegramID: 12345, Role: auth.RoleAdministrator, Active: true}
	database := &databaseStub{row: &rowStub{values: []any{int64(12345), string(auth.RoleAdministrator), false, true}}}
	store := NewAuthStore(database)

	got, err := store.FindActive(context.Background(), 12345)
	if err != nil {
		t.Fatalf("FindActive() error = %v", err)
	}
	if got != want {
		t.Fatalf("FindActive() = %#v, want %#v", got, want)
	}
	if !strings.Contains(database.querySQL, "FROM administrators") || !strings.Contains(database.querySQL, "active") {
		t.Fatalf("FindActive() query = %q", database.querySQL)
	}
}

func TestAuthStoreEnsuresEnvironmentOwnerIsActiveRootOwner(t *testing.T) {
	database := &databaseStub{}
	store := NewAuthStore(database)

	if err := store.EnsureRootOwner(context.Background(), 12345); err != nil {
		t.Fatalf("EnsureRootOwner() error = %v", err)
	}
	if !strings.Contains(database.execSQL, "ON CONFLICT (telegram_id)") || !strings.Contains(database.execSQL, "is_root = TRUE") || !strings.Contains(database.execSQL, "active = TRUE") {
		t.Fatalf("EnsureRootOwner() does not repair protected owner state: %q", database.execSQL)
	}
}

func TestAuthStoreListsActiveAdministratorIDsInStableOrder(t *testing.T) {
	database := &databaseStub{row: &rowStub{values: []any{[]int64{101, 202}}}}
	store := NewAuthStore(database)

	ids, err := store.ActiveAdministratorIDs(context.Background())
	if err != nil {
		t.Fatalf("ActiveAdministratorIDs() error = %v", err)
	}
	if !reflect.DeepEqual(ids, []int64{101, 202}) {
		t.Fatalf("ActiveAdministratorIDs() = %v", ids)
	}
	if !strings.Contains(database.querySQL, "FROM administrators") ||
		!strings.Contains(database.querySQL, "WHERE active") ||
		!strings.Contains(database.querySQL, "ORDER BY telegram_id") {
		t.Fatalf("ActiveAdministratorIDs() query = %q", database.querySQL)
	}
}

type databaseStub struct {
	execSQL   string
	execArgs  []any
	execErr   error
	querySQL  string
	queryArgs []any
	row       pgx.Row
}

func (stub *databaseStub) Exec(_ context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	stub.execSQL = sql
	stub.execArgs = arguments
	return pgconn.CommandTag{}, stub.execErr
}

func (stub *databaseStub) QueryRow(_ context.Context, sql string, arguments ...any) pgx.Row {
	stub.querySQL = sql
	stub.queryArgs = arguments
	return stub.row
}

type rowStub struct {
	values []any
	err    error
}

func (stub *rowStub) Scan(destinations ...any) error {
	if stub.err != nil {
		return stub.err
	}
	if len(destinations) != len(stub.values) {
		return errors.New("unexpected scan destination count")
	}
	for index, destination := range destinations {
		switch pointer := destination.(type) {
		case *int64:
			*pointer = stub.values[index].(int64)
		case *string:
			*pointer = stub.values[index].(string)
		case *bool:
			*pointer = stub.values[index].(bool)
		case *[]int64:
			*pointer = stub.values[index].([]int64)
		case *int:
			*pointer = stub.values[index].(int)
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}
