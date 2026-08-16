package secrets

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type errorReader struct {
	err error
}

func (reader *errorReader) Read([]byte) (int, error) {
	return 0, reader.err
}

func TestValueCipherRoundTripsWithoutPersistingPlaintext(t *testing.T) {
	cipher, err := NewValueCipher(bytes.Repeat([]byte{7}, 32), bytes.NewReader(bytes.Repeat([]byte{3}, 12)))
	if err != nil {
		t.Fatalf("NewValueCipher() error = %v", err)
	}

	sealed, err := cipher.Seal("sing-box/reality-private-key", "private-key-material")
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if len(sealed.Nonce) != 12 || len(sealed.Ciphertext) <= 16 {
		t.Fatalf("Seal() = %#v", sealed)
	}
	if bytes.Contains(sealed.Ciphertext, []byte("private-key-material")) {
		t.Fatal("ciphertext contains plaintext")
	}
	opened, err := cipher.Open("sing-box/reality-private-key", sealed)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if opened != "private-key-material" {
		t.Fatalf("Open() = %q", opened)
	}
}

func TestValueCipherBindsCiphertextToPurposeAndUsesFreshNonce(t *testing.T) {
	cipher, err := NewValueCipher(
		bytes.Repeat([]byte{7}, 32),
		bytes.NewReader(append(bytes.Repeat([]byte{1}, 12), bytes.Repeat([]byte{2}, 12)...)),
	)
	if err != nil {
		t.Fatalf("NewValueCipher() error = %v", err)
	}
	first, err := cipher.Seal("first-purpose", "same-value")
	if err != nil {
		t.Fatalf("first Seal() error = %v", err)
	}
	second, err := cipher.Seal("first-purpose", "same-value")
	if err != nil {
		t.Fatalf("second Seal() error = %v", err)
	}
	if bytes.Equal(first.Nonce, second.Nonce) || bytes.Equal(first.Ciphertext, second.Ciphertext) {
		t.Fatal("Seal() reused nonce or ciphertext")
	}
	if _, err := cipher.Open("second-purpose", first); err == nil {
		t.Fatal("Open() accepted ciphertext under another purpose")
	}
}

func TestValueCipherRejectsInvalidInputsBeforeReadingRandomness(t *testing.T) {
	if _, err := NewValueCipher(bytes.Repeat([]byte{1}, 31), strings.NewReader("")); err == nil {
		t.Fatal("NewValueCipher() accepted a short key")
	}
	cipher, err := NewValueCipher(bytes.Repeat([]byte{1}, 32), &errorReader{err: errors.New("random read")})
	if err != nil {
		t.Fatalf("NewValueCipher() error = %v", err)
	}
	for _, test := range []struct {
		purpose string
		value   string
	}{
		{purpose: "", value: "secret"},
		{purpose: "  ", value: "secret"},
		{purpose: "purpose", value: ""},
	} {
		if _, err := cipher.Seal(test.purpose, test.value); err == nil || strings.Contains(err.Error(), "random read") {
			t.Fatalf("Seal(%q, %q) error = %v", test.purpose, test.value, err)
		}
	}
	if _, err := (ValueCipher{}).Open("purpose", SealedValue{Nonce: make([]byte, 12), Ciphertext: make([]byte, 17)}); err == nil {
		t.Fatal("zero-value cipher accepted Open")
	}
}
