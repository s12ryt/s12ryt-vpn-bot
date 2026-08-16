package backup

import (
	"bytes"
	"testing"
)

func TestArchiveRoundTripAndTamperDetection(t *testing.T) {
	master := bytes.Repeat([]byte{0x42}, 32)
	plaintext := []byte("postgres custom archive\x00secret rows")
	a, err := NewArchive(master, bytes.NewReader(bytes.Repeat([]byte{0x24}, 24)))
	if err != nil {
		t.Fatalf("NewArchive() error = %v", err)
	}

	sealed, err := a.Seal(plaintext)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if bytes.Contains(sealed, plaintext) {
		t.Fatal("sealed archive contains plaintext")
	}
	opened, err := a.Open(sealed)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("Open() = %q, want %q", opened, plaintext)
	}

	sealed[len(sealed)-1] ^= 0x01
	if opened, err = a.Open(sealed); err == nil || opened != nil {
		t.Fatalf("tampered Open() = %q, %v; want nil error result", opened, err)
	}
}

func TestArchiveUsesFreshNonceAndRejectsWrongKey(t *testing.T) {
	random := append(bytes.Repeat([]byte{0x11}, 12), bytes.Repeat([]byte{0x22}, 12)...)
	a, err := NewArchive(bytes.Repeat([]byte{0x33}, 32), bytes.NewReader(random))
	if err != nil {
		t.Fatalf("NewArchive() error = %v", err)
	}

	first, err := a.Seal([]byte("same"))
	if err != nil {
		t.Fatalf("first Seal() error = %v", err)
	}
	second, err := a.Seal([]byte("same"))
	if err != nil {
		t.Fatalf("second Seal() error = %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("two archives reused the same nonce")
	}

	wrong, err := NewArchive(bytes.Repeat([]byte{0x34}, 32), nil)
	if err != nil {
		t.Fatalf("wrong NewArchive() error = %v", err)
	}
	if plaintext, err := wrong.Open(first); err == nil || plaintext != nil {
		t.Fatalf("wrong-key Open() = %q, %v; want failure", plaintext, err)
	}
}

func TestArchiveRejectsInvalidInputs(t *testing.T) {
	if _, err := NewArchive(make([]byte, 31), nil); err == nil {
		t.Fatal("NewArchive() accepted a short master key")
	}
	a, err := NewArchive(make([]byte, 32), nil)
	if err != nil {
		t.Fatalf("NewArchive() error = %v", err)
	}
	if _, err := a.Seal(nil); err == nil {
		t.Fatal("Seal() accepted empty plaintext")
	}
	if plaintext, err := a.Open([]byte("short")); err == nil || plaintext != nil {
		t.Fatalf("Open(short) = %q, %v; want failure", plaintext, err)
	}
	var zero Archive
	if _, err := zero.Seal([]byte("data")); err == nil {
		t.Fatal("zero Archive accepted Seal")
	}
}
