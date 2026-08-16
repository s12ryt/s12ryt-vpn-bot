package config

import (
	"crypto/sha256"
	"errors"
	"io"

	"golang.org/x/crypto/hkdf"
)

const derivedKeyBytes = 32

func DeriveKey(masterKey []byte, purpose string) ([]byte, error) {
	if len(masterKey) != 32 {
		return nil, errors.New("master key must be 32 bytes")
	}
	if purpose == "" {
		return nil, errors.New("key purpose is required")
	}
	reader := hkdf.New(sha256.New, masterKey, []byte("s12ryt-vpn-bot/v1"), []byte(purpose))
	key := make([]byte, derivedKeyBytes)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	return key, nil
}
