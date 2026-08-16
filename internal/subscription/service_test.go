package subscription

import (
	"context"
	"errors"
	"testing"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/singbox"
)

type credentialReaderStub struct {
	bundle domain.CredentialBundle
	err    error
	token  string
}

func (s *credentialReaderStub) FindActiveBySubscriptionToken(_ context.Context, token string) (domain.CredentialBundle, error) {
	s.token = token
	return s.bundle, s.err
}

type settingsReaderStub struct {
	settings singbox.Settings
	err      error
}

func (s settingsReaderStub) Load(context.Context) (singbox.Settings, error) { return s.settings, s.err }

func TestServiceRendersNegotiatedFormats(t *testing.T) {
	settings, bundle := subscriptionFixture(t)
	credentials := &credentialReaderStub{bundle: bundle}
	service, err := NewService(credentials, settingsReaderStub{settings: settings}, Renderer{})
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []Format{FormatBase64, FormatSingBox, FormatClash} {
		body, err := service.Render(context.Background(), "private-token", format)
		if err != nil || len(body) == 0 {
			t.Fatalf("Render(%s) = %d bytes, %v", format, len(body), err)
		}
	}
	if credentials.token != "private-token" {
		t.Fatalf("token = %q", credentials.token)
	}
}

func TestServiceStopsBeforeSettingsWhenCredentialLookupFails(t *testing.T) {
	want := errors.New("lookup")
	service, _ := NewService(&credentialReaderStub{err: want}, settingsReaderStub{err: errors.New("must not replace")}, Renderer{})
	if _, err := service.Render(context.Background(), "token", FormatBase64); !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
}

func TestServiceRejectsUnknownFormat(t *testing.T) {
	service, _ := NewService(&credentialReaderStub{}, settingsReaderStub{}, Renderer{})
	if _, err := service.Render(context.Background(), "token", Format("other")); err == nil {
		t.Fatal("error = nil")
	}
}
