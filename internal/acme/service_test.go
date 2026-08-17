package acme

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"
)

func TestServiceFallsBackAcrossCAsAndInstallsValidatedCertificate(t *testing.T) {
	now := time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
	issued := certificateFixture(t, "vpn.example.com", now.Add(-time.Hour), now.Add(90*24*time.Hour))
	issuer := &issuerStub{results: []issuerResult{{err: errors.New("first CA unavailable")}, {certificate: issued}}}
	installer := &installerStub{}
	service, err := NewService(issuer, installer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := service.Obtain(context.Background(), Settings{
		Mode: ModeCustom, Domain: "vpn.example.com", Challenge: ChallengeHTTP01,
		CADirectoryURLs: []string{"https://ca-one.example/directory", "https://ca-two.example/directory"},
		TermsAccepted:   true,
	})
	if err != nil {
		t.Fatalf("obtain: %v", err)
	}
	if result.CADirectoryURL != "https://ca-two.example/directory" || !result.NotAfter.Equal(issued.NotAfter) {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(issuer.requests) != 2 || issuer.requests[0].Email != "" {
		t.Fatalf("unexpected requests: %+v", issuer.requests)
	}
	if installer.calls != 1 || string(installer.certificate.CertificatePEM) != string(issued.CertificatePEM) {
		t.Fatal("validated certificate was not installed once")
	}
}

func TestServiceRejectsUnsafeSettingsBeforeIssuer(t *testing.T) {
	now := time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
	tests := []Settings{
		{Mode: ModeCustom, Domain: "vpn.example.com", Challenge: ChallengeHTTP01, CADirectoryURLs: []string{"https://ca.example/directory"}},
		{Mode: ModeDuckDNS, Domain: "node.duckdns.org", Challenge: ChallengeDNS01, CADirectoryURLs: []string{"https://ca.example/directory"}, TermsAccepted: true},
		{Mode: ModeSSLIP, Domain: "192-0-2-1.sslip.io", Challenge: ChallengeDNS01, CADirectoryURLs: []string{"https://ca.example/directory"}, TermsAccepted: true},
		{Mode: ModeCustom, Domain: "https://vpn.example.com", Challenge: ChallengeHTTP01, CADirectoryURLs: []string{"https://ca.example/directory"}, TermsAccepted: true},
	}
	for _, settings := range tests {
		issuer := &issuerStub{}
		service, err := NewService(issuer, &installerStub{}, func() time.Time { return now })
		if err != nil {
			t.Fatalf("new service: %v", err)
		}
		if _, err := service.Obtain(context.Background(), settings); err == nil {
			t.Fatalf("expected rejection for %+v", settings)
		}
		if len(issuer.requests) != 0 {
			t.Fatal("issuer called for invalid settings")
		}
	}
}

func TestServiceDoesNotInstallInvalidOrFailedCertificate(t *testing.T) {
	now := time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
	wrongDomain := certificateFixture(t, "other.example.com", now.Add(-time.Hour), now.Add(24*time.Hour))
	issuer := &issuerStub{results: []issuerResult{{certificate: wrongDomain}, {err: errors.New("second failed")}}}
	installer := &installerStub{}
	service, _ := NewService(issuer, installer, func() time.Time { return now })
	_, err := service.Obtain(context.Background(), Settings{Mode: ModeCustom, Domain: "vpn.example.com", Challenge: ChallengeHTTP01, CADirectoryURLs: []string{"https://one.example/directory", "https://two.example/directory"}, TermsAccepted: true})
	if err == nil {
		t.Fatal("expected all issuers to fail")
	}
	if installer.calls != 0 {
		t.Fatal("invalid certificate must not be installed")
	}
}

type issuerResult struct {
	certificate Certificate
	err         error
}
type issuerStub struct {
	requests []Request
	results  []issuerResult
}

func (stub *issuerStub) Obtain(_ context.Context, request Request) (Certificate, error) {
	stub.requests = append(stub.requests, request)
	index := len(stub.requests) - 1
	if index >= len(stub.results) {
		return Certificate{}, errors.New("not configured")
	}
	return stub.results[index].certificate, stub.results[index].err
}

type installerStub struct {
	calls       int
	certificate Certificate
}

func (stub *installerStub) Install(_ context.Context, certificate Certificate) error {
	stub.calls++
	stub.certificate = certificate
	return nil
}

func certificateFixture(t *testing.T, domain string, notBefore, notAfter time.Time) Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: domain}, DNSNames: []string{domain}, NotBefore: notBefore, NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return Certificate{CertificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), PrivateKeyPEM: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), NotAfter: notAfter}
}
