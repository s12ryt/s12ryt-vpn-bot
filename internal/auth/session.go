package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"time"
)

const (
	sessionTokenBytes       = 32
	sessionIdleTimeout      = 12 * time.Hour
	sessionAbsoluteLifetime = 7 * 24 * time.Hour
)

var ErrSessionInvalid = errors.New("session is invalid or expired")

type SessionRecord struct {
	Digest            [32]byte
	Administrator     Administrator
	CreatedAt         time.Time
	LastSeenAt        time.Time
	AbsoluteExpiresAt time.Time
}

type SessionStore interface {
	Create(ctx context.Context, record SessionRecord) error
	AuthenticateAndTouch(ctx context.Context, digest [32]byte, now time.Time, idleTimeout time.Duration) (Administrator, error)
	Delete(ctx context.Context, digest [32]byte) error
	DeleteAll(ctx context.Context, telegramID int64) error
}

type SessionService struct {
	random  io.Reader
	hashKey []byte
	store   SessionStore
	now     func() time.Time
}

func NewSessionService(randomSource io.Reader, hashKey []byte, store SessionStore, now func() time.Time) (*SessionService, error) {
	if len(hashKey) != 32 {
		return nil, errors.New("session hash key must be 32 bytes")
	}
	if store == nil {
		return nil, errors.New("session store is required")
	}
	if randomSource == nil {
		randomSource = rand.Reader
	}
	if now == nil {
		now = time.Now
	}
	return &SessionService{
		random:  randomSource,
		hashKey: append([]byte(nil), hashKey...),
		store:   store,
		now:     now,
	}, nil
}

func (service *SessionService) Create(ctx context.Context, administrator Administrator) (string, error) {
	if administrator.TelegramID <= 0 || !administrator.Active || !administrator.Role.valid() {
		return "", ErrAdministratorUnauthorized
	}
	rawToken := make([]byte, sessionTokenBytes)
	if _, err := io.ReadFull(service.random, rawToken); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	now := service.now()
	record := SessionRecord{
		Digest:            service.digest(token),
		Administrator:     administrator,
		CreatedAt:         now,
		LastSeenAt:        now,
		AbsoluteExpiresAt: now.Add(sessionAbsoluteLifetime),
	}
	if err := service.store.Create(ctx, record); err != nil {
		return "", err
	}
	return token, nil
}

func (service *SessionService) Authenticate(ctx context.Context, token string) (Administrator, error) {
	if !validSessionToken(token) {
		return Administrator{}, ErrSessionInvalid
	}
	administrator, err := service.store.AuthenticateAndTouch(ctx, service.digest(token), service.now(), sessionIdleTimeout)
	if err != nil || administrator.TelegramID <= 0 || !administrator.Active || !administrator.Role.valid() {
		return Administrator{}, ErrSessionInvalid
	}
	return administrator, nil
}

func (service *SessionService) Revoke(ctx context.Context, token string) error {
	if !validSessionToken(token) {
		return ErrSessionInvalid
	}
	return service.store.Delete(ctx, service.digest(token))
}

func (service *SessionService) RevokeAll(ctx context.Context, telegramID int64) error {
	if telegramID <= 0 {
		return ErrSessionInvalid
	}
	return service.store.DeleteAll(ctx, telegramID)
}

func (service *SessionService) digest(token string) [32]byte {
	mac := hmac.New(sha256.New, service.hashKey)
	_, _ = mac.Write([]byte(token))
	var digest [32]byte
	copy(digest[:], mac.Sum(nil))
	return digest
}

func validSessionToken(token string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(decoded) == sessionTokenBytes && base64.RawURLEncoding.EncodeToString(decoded) == token
}
