package acme

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"strings"
	"testing"
)

func generateAccountKeyForTest(t *testing.T) crypto.Signer {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate account key: %v", err)
	}
	return key
}

func TestBuildDNSProviderOnlySupportsConfiguredProviders(t *testing.T) {
	provider, err := buildDNSProvider("duckdns", "duckdns-token")
	if err != nil {
		t.Fatalf("duckdns provider: %v", err)
	}
	if provider == nil {
		t.Fatal("duckdns provider must not be nil")
	}
	for _, invalid := range []struct {
		name  string
		token string
	}{
		{name: "duckdns", token: ""},
		{name: "duckdns", token: "   "},
		{name: "route53", token: "token"},
		{name: "", token: "token"},
	} {
		if _, err := buildDNSProvider(invalid.name, invalid.token); err == nil {
			t.Fatalf("expected rejection for provider %q token %q", invalid.name, invalid.token)
		}
	}
}

func TestLegoIssuerRejectsInvalidRequestsBeforeNetwork(t *testing.T) {
	issuer, err := NewLegoIssuer()
	if err != nil {
		t.Fatalf("new issuer: %v", err)
	}
	requests := []Request{
		{CADirectoryURL: "", Domain: "vpn.example.com", Challenge: ChallengeHTTP01},
		{CADirectoryURL: "http://ca.example/directory", Domain: "vpn.example.com", Challenge: ChallengeHTTP01},
		{CADirectoryURL: "https://ca.example/directory", Domain: "", Challenge: ChallengeHTTP01},
		{CADirectoryURL: "https://ca.example/directory", Domain: "vpn.example.com", Challenge: Challenge("tls_alpn_01")},
		{CADirectoryURL: "https://ca.example/directory", Domain: "vpn.example.com", Challenge: ChallengeDNS01},
	}
	for _, request := range requests {
		_, err := issuer.Obtain(context.Background(), request)
		if err == nil {
			t.Fatalf("expected rejection for %+v", request)
		}
		if strings.Contains(err.Error(), "token") || strings.Contains(err.Error(), "secret") {
			t.Fatalf("error must not leak secret material: %v", err)
		}
	}
}

func TestLegoIssuerUserExposesAccountIdentity(t *testing.T) {
	key := generateAccountKeyForTest(t)
	user := &legoAccountUser{email: "owner@example.com", key: key}
	if user.GetEmail() != "owner@example.com" {
		t.Fatalf("unexpected email %q", user.GetEmail())
	}
	if user.GetRegistration() != nil {
		t.Fatal("registration starts empty")
	}
	if user.GetPrivateKey() == nil {
		t.Fatal("private key must be exposed")
	}
}
