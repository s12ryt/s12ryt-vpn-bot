package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"time"
)

const (
	loginCodeLength   = 8
	loginCodeLifetime = 5 * time.Minute
	loginCodeAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
)

var (
	ErrAdministratorUnauthorized = errors.New("administrator is not authorized")
	ErrLoginCodeInvalid          = errors.New("login code is invalid or expired")
)

type Administrator struct {
	TelegramID int64
	Role       Role
	Root       bool
	Active     bool
}

type LoginCodeRecord struct {
	TelegramID    int64
	Administrator Administrator
	Digest        [32]byte
	ExpiresAt     time.Time
}

type AdministratorLookup interface {
	FindActive(ctx context.Context, telegramID int64) (Administrator, error)
}

type LoginCodeStore interface {
	Replace(ctx context.Context, record LoginCodeRecord) error
	Consume(ctx context.Context, telegramID int64, digest [32]byte, now time.Time) (Administrator, error)
}

type LoginCodeService struct {
	random  io.Reader
	hashKey []byte
	admins  AdministratorLookup
	codes   LoginCodeStore
	now     func() time.Time
}

func NewLoginCodeService(randomSource io.Reader, hashKey []byte, admins AdministratorLookup, codes LoginCodeStore, now func() time.Time) (*LoginCodeService, error) {
	if len(hashKey) != 32 {
		return nil, errors.New("login code hash key must be 32 bytes")
	}
	if admins == nil || codes == nil {
		return nil, errors.New("administrator and login code stores are required")
	}
	if randomSource == nil {
		randomSource = rand.Reader
	}
	if now == nil {
		now = time.Now
	}
	return &LoginCodeService{
		random:  randomSource,
		hashKey: append([]byte(nil), hashKey...),
		admins:  admins,
		codes:   codes,
		now:     now,
	}, nil
}

func (service *LoginCodeService) Issue(ctx context.Context, telegramID int64) (string, error) {
	administrator, err := service.admins.FindActive(ctx, telegramID)
	if err != nil || administrator.TelegramID != telegramID || !administrator.Active || !administrator.Role.valid() {
		return "", ErrAdministratorUnauthorized
	}
	code, err := service.generateCode()
	if err != nil {
		return "", err
	}
	now := service.now()
	record := LoginCodeRecord{
		TelegramID:    telegramID,
		Administrator: administrator,
		Digest:        service.digest(code),
		ExpiresAt:     now.Add(loginCodeLifetime),
	}
	if err := service.codes.Replace(ctx, record); err != nil {
		return "", err
	}
	return code, nil
}

func (service *LoginCodeService) Consume(ctx context.Context, telegramID int64, code string) (Administrator, error) {
	if telegramID <= 0 || !validLoginCode(code) {
		return Administrator{}, ErrLoginCodeInvalid
	}
	return service.codes.Consume(ctx, telegramID, service.digest(code), service.now())
}

func (service *LoginCodeService) generateCode() (string, error) {
	code := make([]byte, 0, loginCodeLength)
	buffer := []byte{0}
	for len(code) < loginCodeLength {
		if _, err := io.ReadFull(service.random, buffer); err != nil {
			return "", err
		}
		index := buffer[0] & 63
		if index >= byte(len(loginCodeAlphabet)) {
			continue
		}
		code = append(code, loginCodeAlphabet[index])
	}
	return string(code), nil
}

func (service *LoginCodeService) digest(code string) [32]byte {
	mac := hmac.New(sha256.New, service.hashKey)
	_, _ = mac.Write([]byte(code))
	var digest [32]byte
	copy(digest[:], mac.Sum(nil))
	return digest
}

func validLoginCode(code string) bool {
	if len(code) != loginCodeLength {
		return false
	}
	for index := range len(code) {
		character := code[index]
		if !((character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9')) {
			return false
		}
	}
	return true
}

func (role Role) valid() bool {
	return role == RoleOwner || role == RoleAdministrator
}
