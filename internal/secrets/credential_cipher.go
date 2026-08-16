package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

const credentialKeyBytes = 32

type SealedCredentialBundle struct {
	SubscriptionTokenDigest [sha256.Size]byte
	Nonce                   []byte
	Ciphertext              []byte
}

type CredentialCipher struct {
	aead      cipher.AEAD
	digestKey []byte
	random    io.Reader
}

func NewCredentialCipher(encryptionKey, digestKey []byte, random io.Reader) (CredentialCipher, error) {
	if len(encryptionKey) != credentialKeyBytes {
		return CredentialCipher{}, errors.New("credential encryption key must be 32 bytes")
	}
	if len(digestKey) != credentialKeyBytes {
		return CredentialCipher{}, errors.New("credential digest key must be 32 bytes")
	}
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return CredentialCipher{}, fmt.Errorf("create credential cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return CredentialCipher{}, fmt.Errorf("create credential AEAD: %w", err)
	}
	if random == nil {
		random = rand.Reader
	}
	return CredentialCipher{
		aead:      aead,
		digestKey: append([]byte(nil), digestKey...),
		random:    random,
	}, nil
}

func (c CredentialCipher) Seal(telegramID int64, generation uint64, bundle domain.CredentialBundle) (SealedCredentialBundle, error) {
	if c.aead == nil || len(c.digestKey) != credentialKeyBytes || c.random == nil {
		return SealedCredentialBundle{}, errors.New("credential cipher is not initialized")
	}
	if err := validateCredentialInput(telegramID, generation, bundle); err != nil {
		return SealedCredentialBundle{}, err
	}
	plaintext, err := json.Marshal(bundle)
	if err != nil {
		return SealedCredentialBundle{}, fmt.Errorf("encode credential bundle: %w", err)
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(c.random, nonce); err != nil {
		return SealedCredentialBundle{}, fmt.Errorf("read credential nonce: %w", err)
	}
	tokenDigest, err := c.SubscriptionTokenDigest(bundle.SubscriptionToken)
	if err != nil {
		return SealedCredentialBundle{}, err
	}

	return SealedCredentialBundle{
		SubscriptionTokenDigest: tokenDigest,
		Nonce:                   nonce,
		Ciphertext:              c.aead.Seal(nil, nonce, plaintext, credentialAAD(telegramID, generation)),
	}, nil
}

func (c CredentialCipher) SubscriptionTokenDigest(token string) ([sha256.Size]byte, error) {
	if len(c.digestKey) != credentialKeyBytes {
		return [sha256.Size]byte{}, errors.New("credential cipher is not initialized")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != credentialKeyBytes || base64.RawURLEncoding.EncodeToString(decoded) != token {
		return [sha256.Size]byte{}, errors.New("subscription token is invalid")
	}
	digest := hmac.New(sha256.New, c.digestKey)
	_, _ = digest.Write([]byte(token))
	var tokenDigest [sha256.Size]byte
	copy(tokenDigest[:], digest.Sum(nil))
	return tokenDigest, nil
}

func (c CredentialCipher) Open(telegramID int64, generation uint64, nonce, ciphertext []byte) (domain.CredentialBundle, error) {
	if c.aead == nil || len(c.digestKey) != credentialKeyBytes {
		return domain.CredentialBundle{}, errors.New("credential cipher is not initialized")
	}
	if telegramID <= 0 || generation == 0 || len(nonce) != c.aead.NonceSize() || len(ciphertext) <= c.aead.Overhead() {
		return domain.CredentialBundle{}, errors.New("invalid sealed credential bundle")
	}
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, credentialAAD(telegramID, generation))
	if err != nil {
		return domain.CredentialBundle{}, errors.New("open credential bundle")
	}
	var bundle domain.CredentialBundle
	if err := json.Unmarshal(plaintext, &bundle); err != nil {
		return domain.CredentialBundle{}, errors.New("decode credential bundle")
	}
	if err := validateCredentialInput(telegramID, generation, bundle); err != nil {
		return domain.CredentialBundle{}, errors.New("invalid credential bundle")
	}
	return bundle, nil
}

func validateCredentialInput(telegramID int64, generation uint64, bundle domain.CredentialBundle) error {
	if telegramID <= 0 {
		return errors.New("Telegram ID must be positive")
	}
	if generation == 0 {
		return errors.New("credential generation must be positive")
	}
	if bundle.SubscriptionToken == "" || bundle.VLESSUUID == "" || bundle.Hysteria2Password == "" ||
		bundle.TUICUUID == "" || bundle.TUICPassword == "" || bundle.AnyTLSPassword == "" {
		return errors.New("credential bundle is incomplete")
	}
	return nil
}

func credentialAAD(telegramID int64, generation uint64) []byte {
	additionalData := make([]byte, 24)
	copy(additionalData[:8], "cred-v1\x00")
	binary.BigEndian.PutUint64(additionalData[8:], uint64(telegramID))
	binary.BigEndian.PutUint64(additionalData[16:], generation)
	return additionalData
}
