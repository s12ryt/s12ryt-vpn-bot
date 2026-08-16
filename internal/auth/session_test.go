package auth

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"
)

func TestSessionServiceCreatesHashedSessionWithRequiredLifetimes(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	store := newFakeSessionStore()
	service, err := NewSessionService(&sessionTokenReader{}, []byte("abcdef0123456789abcdef0123456789"), store, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewSessionService() error = %v", err)
	}
	administrator := Administrator{TelegramID: 12345, Role: RoleOwner, Root: true, Active: true}

	token, err := service.Create(context.Background(), administrator)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(token) {
		t.Fatalf("Create() token = %q, want 32-byte unpadded base64url", token)
	}
	record := store.records[service.digest(token)]
	if record.Digest == ([32]byte{}) {
		t.Fatal("stored digest is empty")
	}
	if !record.CreatedAt.Equal(now) || !record.LastSeenAt.Equal(now) {
		t.Fatalf("stored timestamps = %#v", record)
	}
	if !record.AbsoluteExpiresAt.Equal(now.Add(7 * 24 * time.Hour)) {
		t.Fatalf("AbsoluteExpiresAt = %v", record.AbsoluteExpiresAt)
	}
}

func TestSessionServiceAuthenticatesAndRefreshesIdleDeadline(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	store := newFakeSessionStore()
	service, err := NewSessionService(&sessionTokenReader{}, []byte("abcdef0123456789abcdef0123456789"), store, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewSessionService() error = %v", err)
	}
	want := Administrator{TelegramID: 12345, Role: RoleAdministrator, Active: true}
	token, err := service.Create(context.Background(), want)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now = now.Add(11*time.Hour + 59*time.Minute)

	got, err := service.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if got != want {
		t.Fatalf("Authenticate() = %#v, want %#v", got, want)
	}
	if record := store.records[service.digest(token)]; !record.LastSeenAt.Equal(now) {
		t.Fatalf("LastSeenAt = %v, want %v", record.LastSeenAt, now)
	}
}

func TestSessionServiceRejectsIdleAndAbsoluteExpiryAtBoundary(t *testing.T) {
	for _, tt := range []struct {
		name    string
		advance time.Duration
	}{
		{name: "idle expiry", advance: 12 * time.Hour},
		{name: "absolute expiry", advance: 7 * 24 * time.Hour},
	} {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
			store := newFakeSessionStore()
			service, err := NewSessionService(&sessionTokenReader{}, []byte("abcdef0123456789abcdef0123456789"), store, func() time.Time { return now })
			if err != nil {
				t.Fatalf("NewSessionService() error = %v", err)
			}
			token, err := service.Create(context.Background(), Administrator{TelegramID: 12345, Role: RoleOwner, Active: true})
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			now = now.Add(tt.advance)

			if _, err := service.Authenticate(context.Background(), token); !errors.Is(err, ErrSessionInvalid) {
				t.Fatalf("Authenticate() error = %v, want %v", err, ErrSessionInvalid)
			}
		})
	}
}

func TestSessionServiceRejectsMalformedTokenAndInactiveAdministrator(t *testing.T) {
	store := newFakeSessionStore()
	service, err := NewSessionService(&sessionTokenReader{}, []byte("abcdef0123456789abcdef0123456789"), store, time.Now)
	if err != nil {
		t.Fatalf("NewSessionService() error = %v", err)
	}
	if _, err := service.Create(context.Background(), Administrator{TelegramID: 12345, Role: RoleOwner, Active: false}); !errors.Is(err, ErrAdministratorUnauthorized) {
		t.Fatalf("Create(inactive) error = %v", err)
	}
	if _, err := service.Authenticate(context.Background(), "invalid"); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("Authenticate(malformed) error = %v", err)
	}
}

func TestSessionServiceRevokesOneSessionImmediately(t *testing.T) {
	store := newFakeSessionStore()
	service, err := NewSessionService(&sessionTokenReader{}, []byte("abcdef0123456789abcdef0123456789"), store, time.Now)
	if err != nil {
		t.Fatalf("NewSessionService() error = %v", err)
	}
	token, err := service.Create(context.Background(), Administrator{TelegramID: 12345, Role: RoleOwner, Active: true})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := service.Revoke(context.Background(), token); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if _, err := service.Authenticate(context.Background(), token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("Authenticate(revoked) error = %v, want %v", err, ErrSessionInvalid)
	}
	if err := service.Revoke(context.Background(), "malformed"); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("Revoke(malformed) error = %v, want %v", err, ErrSessionInvalid)
	}
}

func TestSessionServiceRevokesAllSessionsForAdministrator(t *testing.T) {
	store := newFakeSessionStore()
	service, err := NewSessionService(&sessionTokenReader{}, []byte("abcdef0123456789abcdef0123456789"), store, time.Now)
	if err != nil {
		t.Fatalf("NewSessionService() error = %v", err)
	}
	for range 2 {
		if _, err := service.Create(context.Background(), Administrator{TelegramID: 12345, Role: RoleOwner, Active: true}); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}
	if _, err := service.Create(context.Background(), Administrator{TelegramID: 67890, Role: RoleAdministrator, Active: true}); err != nil {
		t.Fatalf("Create(other administrator) error = %v", err)
	}

	if err := service.RevokeAll(context.Background(), 12345); err != nil {
		t.Fatalf("RevokeAll() error = %v", err)
	}
	for _, record := range store.records {
		if record.Administrator.TelegramID == 12345 {
			t.Fatal("RevokeAll() left a target administrator session")
		}
	}
	if len(store.records) != 1 {
		t.Fatalf("remaining sessions = %d, want one unrelated session", len(store.records))
	}
	if err := service.RevokeAll(context.Background(), 0); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("RevokeAll(invalid ID) error = %v, want %v", err, ErrSessionInvalid)
	}
}

type sessionTokenReader struct {
	next byte
}

func (reader *sessionTokenReader) Read(p []byte) (int, error) {
	for i := range p {
		reader.next++
		p[i] = reader.next
	}
	return len(p), nil
}

type fakeSessionStore struct {
	records map[[32]byte]SessionRecord
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{records: make(map[[32]byte]SessionRecord)}
}

func (store *fakeSessionStore) Create(_ context.Context, record SessionRecord) error {
	store.records[record.Digest] = record
	return nil
}

func (store *fakeSessionStore) AuthenticateAndTouch(_ context.Context, digest [32]byte, now time.Time, idleTimeout time.Duration) (Administrator, error) {
	record, ok := store.records[digest]
	if !ok || !now.Before(record.AbsoluteExpiresAt) || !now.Before(record.LastSeenAt.Add(idleTimeout)) || !record.Administrator.Active {
		delete(store.records, digest)
		return Administrator{}, ErrSessionInvalid
	}
	record.LastSeenAt = now
	store.records[digest] = record
	return record.Administrator, nil
}

func (store *fakeSessionStore) Delete(_ context.Context, digest [32]byte) error {
	delete(store.records, digest)
	return nil
}

func (store *fakeSessionStore) DeleteAll(_ context.Context, telegramID int64) error {
	for digest, record := range store.records {
		if record.Administrator.TelegramID == telegramID {
			delete(store.records, digest)
		}
	}
	return nil
}
