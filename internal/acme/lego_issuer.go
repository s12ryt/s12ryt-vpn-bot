package acme

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"net/url"
	"strings"

	"github.com/go-acme/lego/v5/acme"
	"github.com/go-acme/lego/v5/certificate"
	"github.com/go-acme/lego/v5/challenge"
	"github.com/go-acme/lego/v5/challenge/http01"
	"github.com/go-acme/lego/v5/lego"
	"github.com/go-acme/lego/v5/providers/dns/duckdns"
	"github.com/go-acme/lego/v5/registration"
)

const http01ListenAddress = ":80"

// LegoIssuer adapts the official lego ACME client to the issuer contract used
// by the ACME service. Real issuance requires network reachability and is
// validated during post-deploy acceptance; unit tests only cover the offline
// validation and provider mapping paths.
type LegoIssuer struct{}

func NewLegoIssuer() (*LegoIssuer, error) {
	return &LegoIssuer{}, nil
}

type legoAccountUser struct {
	email        string
	registration *acme.ExtendedAccount
	key          crypto.Signer
}

func (user *legoAccountUser) GetEmail() string                       { return user.email }
func (user *legoAccountUser) GetRegistration() *acme.ExtendedAccount { return user.registration }
func (user *legoAccountUser) GetPrivateKey() crypto.Signer           { return user.key }

func (issuer *LegoIssuer) Obtain(ctx context.Context, request Request) (Certificate, error) {
	if issuer == nil {
		return Certificate{}, errors.New("ACME issuer is not initialized")
	}
	if err := validateIssuerRequest(request); err != nil {
		return Certificate{}, err
	}
	accountKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Certificate{}, errors.New("ACME account key generation failed")
	}
	user := &legoAccountUser{email: request.Email, key: accountKey}
	config := lego.NewConfig(user)
	config.CADirURL = request.CADirectoryURL
	config.UserAgent = "s12ryt-vpn-bot"
	client, err := lego.NewClient(config)
	if err != nil {
		return Certificate{}, errors.New("ACME client initialization failed")
	}
	switch request.Challenge {
	case ChallengeHTTP01:
		if err := client.Challenge.SetHTTP01Provider(newHTTP01Provider()); err != nil {
			return Certificate{}, errors.New("ACME HTTP-01 provider initialization failed")
		}
	case ChallengeDNS01:
		provider, err := buildDNSProvider(request.DNSProviderName, request.DNSProviderToken)
		if err != nil {
			return Certificate{}, err
		}
		if err := client.Challenge.SetDNS01Provider(provider); err != nil {
			return Certificate{}, errors.New("ACME DNS-01 provider initialization failed")
		}
	default:
		return Certificate{}, ErrInvalidSettings
	}
	accountRegistration, err := client.Registration.Register(ctx, registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return Certificate{}, errors.New("ACME account registration failed")
	}
	user.registration = accountRegistration
	resource, err := client.Certificate.Obtain(ctx, certificate.ObtainRequest{
		Domains:    []string{request.Domain},
		Bundle:     true,
		MustStaple: false,
	})
	if err != nil || resource == nil {
		return Certificate{}, errors.New("ACME certificate issuance failed")
	}
	return Certificate{
		CertificatePEM: resource.Certificate,
		PrivateKeyPEM:  resource.PrivateKey,
	}, nil
}

func newHTTP01Provider() challenge.Provider {
	return http01.NewProviderServer("", http01ListenAddress)
}

func buildDNSProvider(name, token string) (challenge.Provider, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("DNS provider token is required")
	}
	switch name {
	case "duckdns":
		config := duckdns.NewDefaultConfig()
		config.Token = token
		return duckdns.NewDNSProviderConfig(config)
	default:
		return nil, errors.New("unsupported DNS provider")
	}
}

func validateIssuerRequest(request Request) error {
	parsed, err := url.Parse(strings.TrimSpace(request.CADirectoryURL))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return ErrInvalidSettings
	}
	if strings.TrimSpace(request.Domain) == "" {
		return ErrInvalidSettings
	}
	if request.Challenge != ChallengeHTTP01 && request.Challenge != ChallengeDNS01 {
		return ErrInvalidSettings
	}
	if request.Challenge == ChallengeDNS01 && strings.TrimSpace(request.DNSProviderToken) == "" {
		return ErrInvalidSettings
	}
	return nil
}
