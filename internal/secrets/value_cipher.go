package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strings"
)

type SealedValue struct {
	Nonce      []byte
	Ciphertext []byte
}

type ValueCipher struct {
	aead   cipher.AEAD
	random io.Reader
}

func NewValueCipher(key []byte, random io.Reader) (ValueCipher, error) {
	if len(key) != credentialKeyBytes {
		return ValueCipher{}, errors.New("secret encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return ValueCipher{}, fmt.Errorf("create secret cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return ValueCipher{}, fmt.Errorf("create secret AEAD: %w", err)
	}
	if random == nil {
		random = rand.Reader
	}
	return ValueCipher{aead: aead, random: random}, nil
}

func (c ValueCipher) Seal(purpose, value string) (SealedValue, error) {
	if c.aead == nil || c.random == nil {
		return SealedValue{}, errors.New("secret cipher is not initialized")
	}
	if strings.TrimSpace(purpose) == "" {
		return SealedValue{}, errors.New("secret purpose is required")
	}
	if strings.TrimSpace(value) == "" {
		return SealedValue{}, errors.New("secret value is required")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(c.random, nonce); err != nil {
		return SealedValue{}, fmt.Errorf("read secret nonce: %w", err)
	}
	return SealedValue{
		Nonce:      nonce,
		Ciphertext: c.aead.Seal(nil, nonce, []byte(value), []byte(purpose)),
	}, nil
}

func (c ValueCipher) Open(purpose string, sealed SealedValue) (string, error) {
	if c.aead == nil || strings.TrimSpace(purpose) == "" || len(sealed.Nonce) != c.aead.NonceSize() || len(sealed.Ciphertext) <= c.aead.Overhead() {
		return "", errors.New("invalid sealed secret")
	}
	plaintext, err := c.aead.Open(nil, sealed.Nonce, sealed.Ciphertext, []byte(purpose))
	if err != nil || strings.TrimSpace(string(plaintext)) == "" {
		return "", errors.New("open sealed secret")
	}
	return string(plaintext), nil
}
