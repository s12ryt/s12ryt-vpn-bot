package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/config"
)

var archiveHeader = []byte("S12RYT-BACKUP\x00\x01")

type Archive struct {
	aead   cipher.AEAD
	random io.Reader
}

func NewArchive(masterKey []byte, random io.Reader) (Archive, error) {
	key, err := config.DeriveKey(masterKey, "backup-archive")
	if err != nil {
		return Archive{}, errors.New("derive backup key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return Archive{}, errors.New("initialize backup cipher")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return Archive{}, errors.New("initialize backup archive")
	}
	if random == nil {
		random = rand.Reader
	}
	return Archive{aead: aead, random: random}, nil
}

func (a Archive) Seal(plaintext []byte) ([]byte, error) {
	if a.aead == nil || a.random == nil {
		return nil, errors.New("backup archive is not initialized")
	}
	if len(plaintext) == 0 {
		return nil, errors.New("backup plaintext is empty")
	}
	nonce := make([]byte, a.aead.NonceSize())
	if _, err := io.ReadFull(a.random, nonce); err != nil {
		return nil, errors.New("generate backup nonce")
	}
	sealed := make([]byte, 0, len(archiveHeader)+len(nonce)+len(plaintext)+a.aead.Overhead())
	sealed = append(sealed, archiveHeader...)
	sealed = append(sealed, nonce...)
	sealed = a.aead.Seal(sealed, nonce, plaintext, archiveHeader)
	return sealed, nil
}

func (a Archive) Open(sealed []byte) ([]byte, error) {
	if a.aead == nil {
		return nil, errors.New("backup archive is not initialized")
	}
	prefixSize := len(archiveHeader) + a.aead.NonceSize()
	if len(sealed) < prefixSize+a.aead.Overhead() || !equalBytes(sealed[:len(archiveHeader)], archiveHeader) {
		return nil, errors.New("backup archive is invalid")
	}
	nonce := sealed[len(archiveHeader):prefixSize]
	plaintext, err := a.aead.Open(nil, nonce, sealed[prefixSize:], archiveHeader)
	if err != nil {
		return nil, errors.New("backup archive authentication failed")
	}
	if len(plaintext) == 0 {
		return nil, errors.New("backup archive is empty")
	}
	return plaintext, nil
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var different byte
	for index := range left {
		different |= left[index] ^ right[index]
	}
	return different == 0
}
