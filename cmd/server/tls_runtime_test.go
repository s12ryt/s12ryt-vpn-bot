package main

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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/acme"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/singbox"
)

func TestBuildTLSRuntimeRejectsMissingDependencies(t *testing.T) {
	deps := tlsRuntimeDependencies{
		core:     &coreLoaderStub{settings: singbox.Settings{TLSCertificatePath: "/tmp/fullchain.pem", TLSKeyPath: "/tmp/privkey.pem"}},
		settings: &tlsSettingsLoaderStub{},
		issuer:   &tlsIssuerStub{},
		issuance: &tlsIssuanceRecorderStub{},
		failures: &tlsFailureRecorderStub{},
		now:      time.Now,
	}
	if _, err := buildTLSRuntime(deps); err != nil {
		t.Fatalf("buildTLSRuntime() error = %v", err)
	}
	deps.core = nil
	if _, err := buildTLSRuntime(deps); err == nil {
		t.Fatal("expected missing core loader to be rejected")
	}
	deps.core = &coreLoaderStub{}
	deps.settings = nil
	if _, err := buildTLSRuntime(deps); err == nil {
		t.Fatal("expected missing TLS settings loader to be rejected")
	}
}

func TestReloadingCertificateInstallerUsesConfiguredPaths(t *testing.T) {
	directory := t.TempDir()
	loader := &coreLoaderStub{settings: singbox.Settings{
		TLSCertificatePath: filepath.Join(directory, "fullchain.pem"),
		TLSKeyPath:         filepath.Join(directory, "privkey.pem"),
	}}
	installer := &reloadingCertificateInstaller{core: loader}
	if err := installer.Install(context.Background(), tlsCertificateFixture(t, time.Now().UTC())); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if contents, err := os.ReadFile(filepath.Join(directory, "privkey.pem")); err != nil || !strings.Contains(string(contents), "PRIVATE KEY") {
		t.Fatalf("key file = %q err=%v", contents, err)
	}

	broken := &coreLoaderStub{err: errors.New("core settings unavailable")}
	if err := (&reloadingCertificateInstaller{core: broken}).Install(context.Background(), tlsCertificateFixture(t, time.Now().UTC())); err == nil {
		t.Fatal("expected core loader error to propagate")
	}
}

func TestTLSRuntimeEnsureObtainsAndInstallsEndToEnd(t *testing.T) {
	now := time.Date(2026, 8, 17, 11, 0, 0, 0, time.UTC)
	directory := t.TempDir()
	certificate := tlsCertificateFixture(t, now)
	expires := certificate.NotAfter.Truncate(time.Second)
	deps := tlsRuntimeDependencies{
		core: &coreLoaderStub{settings: singbox.Settings{
			TLSCertificatePath: filepath.Join(directory, "fullchain.pem"),
			TLSKeyPath:         filepath.Join(directory, "privkey.pem"),
		}},
		settings: &tlsSettingsLoaderStub{settings: acme.Settings{
			Mode: acme.ModeCustom, Domain: "vpn.example.com", Challenge: acme.ChallengeHTTP01,
			CADirectoryURLs: []string{"https://ca.example/directory"}, TermsAccepted: true,
		}},
		issuer:   &tlsIssuerStub{certificate: certificate},
		issuance: &tlsIssuanceRecorderStub{},
		failures: &tlsFailureRecorderStub{},
		now:      func() time.Time { return now },
	}
	coordinator, err := buildTLSRuntime(deps)
	if err != nil {
		t.Fatalf("buildTLSRuntime() error = %v", err)
	}

	if err := coordinator.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "fullchain.pem")); err != nil {
		t.Fatalf("certificate was not installed: %v", err)
	}
	recorder := deps.issuance.(*tlsIssuanceRecorderStub)
	if recorder.calls != 1 || !recorder.expiresAt.Equal(expires) {
		t.Fatalf("issuance recorded = %+v want expiry %v", recorder, expires)
	}
	if deps.failures.(*tlsFailureRecorderStub).calls != 0 {
		t.Fatal("failure must not be recorded on success")
	}
}

type coreLoaderStub struct {
	settings singbox.Settings
	err      error
}

func (stub *coreLoaderStub) Load(context.Context) (singbox.Settings, error) {
	return stub.settings, stub.err
}

type tlsSettingsLoaderStub struct {
	settings  acme.Settings
	expiresAt time.Time
	err       error
}

func (stub *tlsSettingsLoaderStub) LoadForIssuance(context.Context) (acme.Settings, time.Time, error) {
	return stub.settings, stub.expiresAt, stub.err
}

type tlsIssuerStub struct {
	certificate acme.Certificate
	err         error
}

func (stub *tlsIssuerStub) Obtain(_ context.Context, _ acme.Request) (acme.Certificate, error) {
	return stub.certificate, stub.err
}

type tlsIssuanceRecorderStub struct {
	calls     int
	caURL     string
	expiresAt time.Time
}

func (stub *tlsIssuanceRecorderStub) RecordIssuance(_ context.Context, caURL string, expiresAt, _ time.Time) error {
	stub.calls++
	stub.caURL, stub.expiresAt = caURL, expiresAt
	return nil
}

type tlsFailureRecorderStub struct {
	calls int
}

func (stub *tlsFailureRecorderStub) RecordFailure(context.Context, string, time.Time) error {
	stub.calls++
	return nil
}

func tlsCertificateFixture(t *testing.T, now time.Time) acme.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "vpn.example.com"},
		DNSNames: []string{"vpn.example.com"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(90 * 24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return acme.Certificate{
		CertificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		PrivateKeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		NotAfter:       template.NotAfter,
	}
}
