package auth

import (
	"context"
	"errors"
	"io"
	"regexp"
	"testing"
	"time"
)

func TestLoginCodeServiceIssuesHashedFiveMinuteSingleUseCode(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	admins := fakeAdministratorLookup{administrators: map[int64]Administrator{
		12345: {TelegramID: 12345, Role: RoleOwner, Active: true},
	}}
	codes := newFakeLoginCodeStore()
	service, err := NewLoginCodeService(&loginCodeReader{}, []byte("0123456789abcdef0123456789abcdef"), admins, codes, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewLoginCodeService() error = %v", err)
	}

	code, err := service.Issue(context.Background(), 12345)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9]{8}$`).MatchString(code) {
		t.Fatalf("Issue() code = %q, want 8 alphanumeric characters", code)
	}
	record := codes.byAdministrator[12345]
	if record.Digest == ([32]byte{}) {
		t.Fatal("stored digest is empty")
	}
	if !record.ExpiresAt.Equal(now.Add(5 * time.Minute)) {
		t.Fatalf("ExpiresAt = %v, want %v", record.ExpiresAt, now.Add(5*time.Minute))
	}

	administrator, err := service.Consume(context.Background(), 12345, code)
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if administrator.TelegramID != 12345 || administrator.Role != RoleOwner {
		t.Fatalf("Consume() administrator = %#v", administrator)
	}
	if _, err := service.Consume(context.Background(), 12345, code); !errors.Is(err, ErrLoginCodeInvalid) {
		t.Fatalf("second Consume() error = %v, want %v", err, ErrLoginCodeInvalid)
	}
}

func TestLoginCodeServiceNewCodeInvalidatesPreviousCode(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	admins := fakeAdministratorLookup{administrators: map[int64]Administrator{
		12345: {TelegramID: 12345, Role: RoleAdministrator, Active: true},
	}}
	codes := newFakeLoginCodeStore()
	service, err := NewLoginCodeService(&loginCodeReader{}, []byte("0123456789abcdef0123456789abcdef"), admins, codes, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewLoginCodeService() error = %v", err)
	}
	first, err := service.Issue(context.Background(), 12345)
	if err != nil {
		t.Fatalf("first Issue() error = %v", err)
	}
	second, err := service.Issue(context.Background(), 12345)
	if err != nil {
		t.Fatalf("second Issue() error = %v", err)
	}
	if first == second {
		t.Fatal("successive Issue() calls returned the same code")
	}
	if _, err := service.Consume(context.Background(), 12345, first); !errors.Is(err, ErrLoginCodeInvalid) {
		t.Fatalf("Consume(old code) error = %v, want %v", err, ErrLoginCodeInvalid)
	}
	if _, err := service.Consume(context.Background(), 12345, second); err != nil {
		t.Fatalf("Consume(new code) error = %v", err)
	}
}

func TestLoginCodeServiceRejectsExpiredAndMalformedCodes(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	admins := fakeAdministratorLookup{administrators: map[int64]Administrator{
		12345: {TelegramID: 12345, Role: RoleOwner, Active: true},
	}}
	codes := newFakeLoginCodeStore()
	service, err := NewLoginCodeService(&loginCodeReader{}, []byte("0123456789abcdef0123456789abcdef"), admins, codes, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewLoginCodeService() error = %v", err)
	}
	code, err := service.Issue(context.Background(), 12345)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	now = now.Add(5 * time.Minute)
	if _, err := service.Consume(context.Background(), 12345, code); !errors.Is(err, ErrLoginCodeInvalid) {
		t.Fatalf("Consume(expired code) error = %v, want %v", err, ErrLoginCodeInvalid)
	}
	if _, err := service.Consume(context.Background(), 12345, "bad-code"); !errors.Is(err, ErrLoginCodeInvalid) {
		t.Fatalf("Consume(malformed code) error = %v, want %v", err, ErrLoginCodeInvalid)
	}
	if _, err := service.Consume(context.Background(), 99999, code); !errors.Is(err, ErrLoginCodeInvalid) {
		t.Fatalf("Consume(wrong administrator) error = %v, want %v", err, ErrLoginCodeInvalid)
	}
}

func TestLoginCodeServiceRejectsUnauthorizedAdministratorWithoutWritingCode(t *testing.T) {
	codes := newFakeLoginCodeStore()
	service, err := NewLoginCodeService(&loginCodeReader{}, []byte("0123456789abcdef0123456789abcdef"), fakeAdministratorLookup{}, codes, time.Now)
	if err != nil {
		t.Fatalf("NewLoginCodeService() error = %v", err)
	}

	if _, err := service.Issue(context.Background(), 99999); !errors.Is(err, ErrAdministratorUnauthorized) {
		t.Fatalf("Issue() error = %v, want %v", err, ErrAdministratorUnauthorized)
	}
	if len(codes.byAdministrator) != 0 {
		t.Fatalf("Issue() wrote %d code records for unauthorized user", len(codes.byAdministrator))
	}
}

type loginCodeReader struct {
	next byte
}

func (reader *loginCodeReader) Read(p []byte) (int, error) {
	for i := range p {
		reader.next++
		p[i] = reader.next
	}
	return len(p), nil
}

type fakeAdministratorLookup struct {
	administrators map[int64]Administrator
}

func (lookup fakeAdministratorLookup) FindActive(_ context.Context, telegramID int64) (Administrator, error) {
	administrator, ok := lookup.administrators[telegramID]
	if !ok || !administrator.Active {
		return Administrator{}, ErrAdministratorUnauthorized
	}
	return administrator, nil
}

type fakeLoginCodeStore struct {
	byAdministrator map[int64]LoginCodeRecord
	byDigest        map[[32]byte]int64
}

func newFakeLoginCodeStore() *fakeLoginCodeStore {
	return &fakeLoginCodeStore{
		byAdministrator: make(map[int64]LoginCodeRecord),
		byDigest:        make(map[[32]byte]int64),
	}
}

func (store *fakeLoginCodeStore) Replace(_ context.Context, record LoginCodeRecord) error {
	if old, ok := store.byAdministrator[record.TelegramID]; ok {
		delete(store.byDigest, old.Digest)
	}
	store.byAdministrator[record.TelegramID] = record
	store.byDigest[record.Digest] = record.TelegramID
	return nil
}

func (store *fakeLoginCodeStore) Consume(_ context.Context, expectedTelegramID int64, digest [32]byte, now time.Time) (Administrator, error) {
	telegramID, ok := store.byDigest[digest]
	if !ok || telegramID != expectedTelegramID {
		return Administrator{}, ErrLoginCodeInvalid
	}
	record := store.byAdministrator[telegramID]
	delete(store.byDigest, digest)
	delete(store.byAdministrator, telegramID)
	if !now.Before(record.ExpiresAt) {
		return Administrator{}, ErrLoginCodeInvalid
	}
	return record.Administrator, nil
}

var _ io.Reader = (*loginCodeReader)(nil)
