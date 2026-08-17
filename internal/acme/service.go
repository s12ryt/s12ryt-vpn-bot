package acme

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Mode string

const (
	ModeSSLIP   Mode = "sslip_io"
	ModeDuckDNS Mode = "duckdns"
	ModeCustom  Mode = "custom"
)

type Challenge string

const (
	ChallengeHTTP01 Challenge = "http_01"
	ChallengeDNS01  Challenge = "dns_01"
)

type Settings struct {
	Mode             Mode
	Domain           string
	Challenge        Challenge
	Email            string
	CADirectoryURLs  []string
	TermsAccepted    bool
	DNSProviderName  string
	DNSProviderToken string
}

type Request struct {
	CADirectoryURL   string
	Domain           string
	Challenge        Challenge
	Email            string
	DNSProviderName  string
	DNSProviderToken string
}

type Certificate struct {
	CertificatePEM []byte
	PrivateKeyPEM  []byte
	NotAfter       time.Time
}

type Result struct {
	CADirectoryURL string
	NotAfter       time.Time
}

type Issuer interface {
	Obtain(context.Context, Request) (Certificate, error)
}

type Installer interface {
	Install(context.Context, Certificate) error
}

type Service struct {
	issuer    Issuer
	installer Installer
	now       func() time.Time
}

var (
	ErrInvalidSettings = errors.New("invalid ACME settings")
	ErrIssuanceFailed  = errors.New("ACME issuance failed")
	// ErrNotConfigured reports that the owner has not completed TLS setup.
	ErrNotConfigured = errors.New("TLS settings are not configured")
	domainPattern    = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)
)

func NewService(issuer Issuer, installer Installer, now func() time.Time) (*Service, error) {
	if issuer == nil || installer == nil {
		return nil, errors.New("ACME service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{issuer: issuer, installer: installer, now: now}, nil
}

func (service *Service) Obtain(ctx context.Context, settings Settings) (Result, error) {
	if service == nil || service.issuer == nil || service.installer == nil || service.now == nil {
		return Result{}, ErrInvalidSettings
	}
	settings, err := ValidateSettings(settings)
	if err != nil {
		return Result{}, err
	}
	for _, caDirectoryURL := range settings.CADirectoryURLs {
		certificate, issueErr := service.issuer.Obtain(ctx, Request{
			CADirectoryURL: caDirectoryURL, Domain: settings.Domain, Challenge: settings.Challenge,
			Email: settings.Email, DNSProviderName: settings.DNSProviderName, DNSProviderToken: settings.DNSProviderToken,
		})
		if issueErr != nil {
			continue
		}
		notAfter, validateErr := validateCertificate(certificate, settings.Domain, service.now().UTC())
		if validateErr != nil {
			continue
		}
		certificate.NotAfter = notAfter
		if err := service.installer.Install(ctx, certificate); err != nil {
			return Result{}, fmt.Errorf("install ACME certificate: %w", err)
		}
		return Result{CADirectoryURL: caDirectoryURL, NotAfter: notAfter}, nil
	}
	return Result{}, ErrIssuanceFailed
}

// ValidateSettings normalizes and enforces the persisted TLS configuration
// rules before any database write or network issuance attempt.
func ValidateSettings(settings Settings) (Settings, error) {
	settings.Domain = strings.ToLower(strings.TrimSpace(settings.Domain))
	settings.Email = strings.TrimSpace(settings.Email)
	settings.DNSProviderName = strings.ToLower(strings.TrimSpace(settings.DNSProviderName))
	settings.DNSProviderToken = strings.TrimSpace(settings.DNSProviderToken)
	if !settings.TermsAccepted || len(settings.Domain) > 253 || !domainPattern.MatchString(settings.Domain) || strings.Contains(settings.Domain, "..") {
		return Settings{}, ErrInvalidSettings
	}
	if settings.Email != "" && (!strings.Contains(settings.Email, "@") || len(settings.Email) > 254) {
		return Settings{}, ErrInvalidSettings
	}
	if len(settings.CADirectoryURLs) == 0 || len(settings.CADirectoryURLs) > 5 {
		return Settings{}, ErrInvalidSettings
	}
	seen := make(map[string]struct{}, len(settings.CADirectoryURLs))
	for index, raw := range settings.CADirectoryURLs {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return Settings{}, ErrInvalidSettings
		}
		normalized := parsed.String()
		if _, exists := seen[normalized]; exists {
			return Settings{}, ErrInvalidSettings
		}
		seen[normalized] = struct{}{}
		settings.CADirectoryURLs[index] = normalized
	}
	switch settings.Mode {
	case ModeSSLIP:
		if settings.Challenge != ChallengeHTTP01 || !strings.HasSuffix(settings.Domain, ".sslip.io") {
			return Settings{}, ErrInvalidSettings
		}
	case ModeDuckDNS:
		if settings.Challenge != ChallengeDNS01 || !strings.HasSuffix(settings.Domain, ".duckdns.org") || settings.DNSProviderToken == "" {
			return Settings{}, ErrInvalidSettings
		}
		settings.DNSProviderName = "duckdns"
	case ModeCustom:
		if settings.Challenge == ChallengeDNS01 && (settings.DNSProviderName == "" || settings.DNSProviderToken == "") {
			return Settings{}, ErrInvalidSettings
		}
		if settings.Challenge != ChallengeHTTP01 && settings.Challenge != ChallengeDNS01 {
			return Settings{}, ErrInvalidSettings
		}
	default:
		return Settings{}, ErrInvalidSettings
	}
	return settings, nil
}

func validateCertificate(certificate Certificate, domain string, now time.Time) (time.Time, error) {
	keyPair, err := tls.X509KeyPair(certificate.CertificatePEM, certificate.PrivateKeyPEM)
	if err != nil || len(keyPair.Certificate) == 0 {
		return time.Time{}, ErrIssuanceFailed
	}
	leaf, err := x509.ParseCertificate(keyPair.Certificate[0])
	if err != nil || leaf.VerifyHostname(domain) != nil || now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return time.Time{}, ErrIssuanceFailed
	}
	return leaf.NotAfter.UTC(), nil
}
