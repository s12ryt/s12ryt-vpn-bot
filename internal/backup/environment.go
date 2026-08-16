package backup

import (
	"encoding/base64"
	"errors"
)

func DecodeMasterKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return nil, errors.New("APP_MASTER_KEY must be standard Base64 encoding of 32 bytes")
	}
	return key, nil
}
