package config

import (
	"bytes"
	"testing"
)

func TestDeriveKeyProvidesDeterministicPurposeIsolation(t *testing.T) {
	masterKey := []byte("0123456789abcdef0123456789abcdef")

	loginCodeKey, err := DeriveKey(masterKey, "admin-login-code")
	if err != nil {
		t.Fatalf("DeriveKey(login) error = %v", err)
	}
	sessionKey, err := DeriveKey(masterKey, "admin-session")
	if err != nil {
		t.Fatalf("DeriveKey(session) error = %v", err)
	}
	loginCodeKeyAgain, err := DeriveKey(masterKey, "admin-login-code")
	if err != nil {
		t.Fatalf("DeriveKey(login again) error = %v", err)
	}
	if len(loginCodeKey) != 32 || len(sessionKey) != 32 {
		t.Fatalf("derived key lengths = %d, %d; want 32", len(loginCodeKey), len(sessionKey))
	}
	if bytes.Equal(loginCodeKey, sessionKey) {
		t.Fatal("different purposes produced the same key")
	}
	if !bytes.Equal(loginCodeKey, loginCodeKeyAgain) {
		t.Fatal("same purpose did not produce a deterministic key")
	}
}

func TestDeriveKeyRejectsInvalidInputs(t *testing.T) {
	if _, err := DeriveKey(make([]byte, 31), "admin-session"); err == nil {
		t.Fatal("DeriveKey() accepted a short master key")
	}
	if _, err := DeriveKey(make([]byte, 32), ""); err == nil {
		t.Fatal("DeriveKey() accepted an empty purpose")
	}
}
