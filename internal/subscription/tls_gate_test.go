package subscription

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/singbox"
)

func TestServiceRefusesToOutputNodesBeforeTLSCertificateIssued(t *testing.T) {
	settings, bundle := subscriptionFixture(t)
	service, err := NewService(&gateCredentialReader{bundle: bundle}, &gateSettingsReader{settings: settings}, Renderer{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service = service.WithTLSReadiness(&tlsReadinessStub{issued: false})

	payload, err := service.Render(context.Background(), bundle.SubscriptionToken, FormatBase64)
	if !errors.Is(err, ErrTLSNotReady) {
		t.Fatalf("Render() error = %v, want ErrTLSNotReady", err)
	}
	if payload != nil {
		t.Fatal("Render() must not output any node payload before TLS is issued")
	}
}

func TestServiceOutputsNodesOnceTLSCertificateIssued(t *testing.T) {
	settings, bundle := subscriptionFixture(t)
	service, err := NewService(&gateCredentialReader{bundle: bundle}, &gateSettingsReader{settings: settings}, Renderer{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service = service.WithTLSReadiness(&tlsReadinessStub{issued: true})

	payload, err := service.Render(context.Background(), bundle.SubscriptionToken, FormatBase64)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	decoded, decodeErr := base64.StdEncoding.DecodeString(string(payload))
	if decodeErr != nil || !strings.Contains(string(decoded), "://") {
		t.Fatalf("Render() payload = %q decodeErr=%v, want node URIs", payload, decodeErr)
	}
}

func TestServicePropagatesTLSReadinessErrors(t *testing.T) {
	settings, bundle := subscriptionFixture(t)
	service, _ := NewService(&gateCredentialReader{bundle: bundle}, &gateSettingsReader{settings: settings}, Renderer{})
	service = service.WithTLSReadiness(&tlsReadinessStub{err: errors.New("database unavailable")})

	if _, err := service.Render(context.Background(), bundle.SubscriptionToken, FormatBase64); err == nil || errors.Is(err, ErrTLSNotReady) {
		t.Fatalf("Render() error = %v, want propagated failure", err)
	}
}

type tlsReadinessStub struct {
	issued bool
	err    error
}

func (stub *tlsReadinessStub) TLSIssued(context.Context) (bool, error) {
	return stub.issued, stub.err
}

type gateCredentialReader struct {
	bundle domain.CredentialBundle
}

func (reader *gateCredentialReader) FindActiveBySubscriptionToken(context.Context, string) (domain.CredentialBundle, error) {
	return reader.bundle, nil
}

type gateSettingsReader struct {
	settings singbox.Settings
}

func (reader *gateSettingsReader) Load(context.Context) (singbox.Settings, error) {
	return reader.settings, nil
}
