package domain

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

const credentialBytes = 32

type CredentialBundle struct {
	SubscriptionToken string
	VLESSUUID         string
	Hysteria2Password string
	TUICUUID          string
	TUICPassword      string
	AnyTLSPassword    string
}

type CredentialIssuer struct {
	random io.Reader
}

func NewCredentialIssuer(random io.Reader) CredentialIssuer {
	if random == nil {
		random = rand.Reader
	}
	return CredentialIssuer{random: random}
}

func (i CredentialIssuer) Issue() (CredentialBundle, error) {
	subscriptionToken, err := i.randomToken()
	if err != nil {
		return CredentialBundle{}, err
	}
	vlessUUID, err := i.randomUUID()
	if err != nil {
		return CredentialBundle{}, err
	}
	hysteria2Password, err := i.randomToken()
	if err != nil {
		return CredentialBundle{}, err
	}
	tuicUUID, err := i.randomUUID()
	if err != nil {
		return CredentialBundle{}, err
	}
	tuicPassword, err := i.randomToken()
	if err != nil {
		return CredentialBundle{}, err
	}
	anyTLSPassword, err := i.randomToken()
	if err != nil {
		return CredentialBundle{}, err
	}

	return CredentialBundle{
		SubscriptionToken: subscriptionToken,
		VLESSUUID:         vlessUUID,
		Hysteria2Password: hysteria2Password,
		TUICUUID:          tuicUUID,
		TUICPassword:      tuicPassword,
		AnyTLSPassword:    anyTLSPassword,
	}, nil
}

func (i CredentialIssuer) randomToken() (string, error) {
	value := make([]byte, credentialBytes)
	if _, err := io.ReadFull(i.random, value); err != nil {
		return "", fmt.Errorf("read random credential: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (i CredentialIssuer) randomUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(i.random, value); err != nil {
		return "", fmt.Errorf("read random UUID: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}
