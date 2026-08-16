package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

const csrfTokenBytes = 32

type LoginCodeConsumer interface {
	Consume(ctx context.Context, telegramID int64, code string) (Administrator, error)
}

type SessionCreator interface {
	Create(ctx context.Context, administrator Administrator) (string, error)
}

type LoginFlow struct {
	random   io.Reader
	codes    LoginCodeConsumer
	sessions SessionCreator
}

func NewLoginFlow(randomSource io.Reader, codes LoginCodeConsumer, sessions SessionCreator) *LoginFlow {
	if randomSource == nil {
		randomSource = rand.Reader
	}
	return &LoginFlow{random: randomSource, codes: codes, sessions: sessions}
}

func (flow *LoginFlow) Exchange(ctx context.Context, telegramID int64, code string) (string, string, error) {
	if flow == nil || flow.codes == nil || flow.sessions == nil {
		return "", "", errors.New("login flow dependencies are required")
	}
	rawCSRFToken := make([]byte, csrfTokenBytes)
	if _, err := io.ReadFull(flow.random, rawCSRFToken); err != nil {
		return "", "", err
	}
	administrator, err := flow.codes.Consume(ctx, telegramID, code)
	if err != nil {
		return "", "", err
	}
	sessionToken, err := flow.sessions.Create(ctx, administrator)
	if err != nil {
		return "", "", err
	}
	return sessionToken, base64.RawURLEncoding.EncodeToString(rawCSRFToken), nil
}
