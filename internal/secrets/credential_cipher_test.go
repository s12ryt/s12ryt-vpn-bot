package secrets

import (
	"bytes"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

func TestCredentialCipherRoundTripsBundleAndBindsOwnerGeneration(t *testing.T) {
	cipher, err := NewCredentialCipher(
		bytes.Repeat([]byte{1}, 32),
		bytes.Repeat([]byte{2}, 32),
		bytes.NewReader(bytes.Repeat([]byte{3}, 24)),
	)
	if err != nil {
		t.Fatalf("NewCredentialCipher() error = %v", err)
	}
	bundle := validCredentialBundle()

	sealed, err := cipher.Seal(12345, 7, bundle)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if len(sealed.Nonce) != 12 || len(sealed.Ciphertext) <= 16 || sealed.SubscriptionTokenDigest == ([32]byte{}) {
		t.Fatalf("Seal() = %#v", sealed)
	}
	if bytes.Contains(sealed.Ciphertext, []byte(bundle.SubscriptionToken)) || bytes.Contains(sealed.Ciphertext, []byte(bundle.AnyTLSPassword)) {
		t.Fatal("ciphertext contains plaintext credential")
	}
	opened, err := cipher.Open(12345, 7, sealed.Nonce, sealed.Ciphertext)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if opened != bundle {
		t.Fatalf("Open() = %#v, want %#v", opened, bundle)
	}
	if _, err := cipher.Open(54321, 7, sealed.Nonce, sealed.Ciphertext); err == nil {
		t.Fatal("Open() accepted ciphertext for another Telegram ID")
	}
	if _, err := cipher.Open(12345, 8, sealed.Nonce, sealed.Ciphertext); err == nil {
		t.Fatal("Open() accepted ciphertext for another generation")
	}
}

func TestCredentialCipherUsesFreshNonceAndStableKeyedTokenDigest(t *testing.T) {
	cipher, err := NewCredentialCipher(
		bytes.Repeat([]byte{1}, 32),
		bytes.Repeat([]byte{2}, 32),
		bytes.NewReader(append(bytes.Repeat([]byte{3}, 12), bytes.Repeat([]byte{4}, 12)...)),
	)
	if err != nil {
		t.Fatalf("NewCredentialCipher() error = %v", err)
	}
	bundle := validCredentialBundle()

	first, err := cipher.Seal(12345, 1, bundle)
	if err != nil {
		t.Fatalf("first Seal() error = %v", err)
	}
	second, err := cipher.Seal(12345, 1, bundle)
	if err != nil {
		t.Fatalf("second Seal() error = %v", err)
	}
	if bytes.Equal(first.Nonce, second.Nonce) || bytes.Equal(first.Ciphertext, second.Ciphertext) {
		t.Fatal("Seal() reused nonce or ciphertext")
	}
	if first.SubscriptionTokenDigest != second.SubscriptionTokenDigest {
		t.Fatal("same subscription token produced different lookup digest")
	}
	digest, err := cipher.SubscriptionTokenDigest(bundle.SubscriptionToken)
	if err != nil {
		t.Fatalf("SubscriptionTokenDigest() error = %v", err)
	}
	if digest != first.SubscriptionTokenDigest {
		t.Fatal("SubscriptionTokenDigest() does not match sealed lookup digest")
	}
	if _, err := cipher.SubscriptionTokenDigest("not-a-token"); err == nil {
		t.Fatal("SubscriptionTokenDigest() accepted malformed token")
	}
}

func TestCredentialCipherRejectsInvalidInputsWithoutReadingRandomness(t *testing.T) {
	random := &countingReader{}
	cipher, err := NewCredentialCipher(bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32), random)
	if err != nil {
		t.Fatalf("NewCredentialCipher() error = %v", err)
	}
	invalid := validCredentialBundle()
	invalid.TUICPassword = ""

	for _, testCase := range []struct {
		telegramID int64
		generation uint64
		bundle     domain.CredentialBundle
	}{
		{telegramID: 0, generation: 1, bundle: validCredentialBundle()},
		{telegramID: 12345, generation: 0, bundle: validCredentialBundle()},
		{telegramID: 12345, generation: 1, bundle: invalid},
	} {
		if _, err := cipher.Seal(testCase.telegramID, testCase.generation, testCase.bundle); err == nil {
			t.Fatalf("Seal() accepted invalid input %#v", testCase)
		}
	}
	if random.calls != 0 {
		t.Fatalf("invalid inputs read randomness %d times", random.calls)
	}
	if _, err := NewCredentialCipher(make([]byte, 31), make([]byte, 32), nil); err == nil {
		t.Fatal("NewCredentialCipher() accepted short encryption key")
	}
	if _, err := NewCredentialCipher(make([]byte, 32), make([]byte, 31), nil); err == nil {
		t.Fatal("NewCredentialCipher() accepted short digest key")
	}
}

func TestCredentialCipherZeroValueFailsClosed(t *testing.T) {
	var cipher CredentialCipher
	if _, err := cipher.Seal(12345, 1, validCredentialBundle()); err == nil {
		t.Fatal("zero-value Seal() succeeded")
	}
	if _, err := cipher.Open(12345, 1, make([]byte, 12), make([]byte, 17)); err == nil {
		t.Fatal("zero-value Open() succeeded")
	}
}

func validCredentialBundle() domain.CredentialBundle {
	return domain.CredentialBundle{
		SubscriptionToken: strings.Repeat("A", 43),
		VLESSUUID:         "11111111-1111-4111-8111-111111111111",
		Hysteria2Password: strings.Repeat("B", 43),
		TUICUUID:          "22222222-2222-4222-8222-222222222222",
		TUICPassword:      strings.Repeat("C", 43),
		AnyTLSPassword:    strings.Repeat("D", 43),
	}
}

type countingReader struct {
	calls int
}

func (reader *countingReader) Read([]byte) (int, error) {
	reader.calls++
	return 0, nil
}
