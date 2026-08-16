package subscription

import (
	"context"
	"errors"
	"fmt"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/singbox"
)

type CredentialReader interface {
	FindActiveBySubscriptionToken(context.Context, string) (domain.CredentialBundle, error)
}
type SettingsReader interface {
	Load(context.Context) (singbox.Settings, error)
}

type Service struct {
	credentials CredentialReader
	settings    SettingsReader
	renderer    Renderer
}

func NewService(credentials CredentialReader, settings SettingsReader, renderer Renderer) (*Service, error) {
	if credentials == nil || settings == nil {
		return nil, errors.New("subscription dependencies are required")
	}
	return &Service{credentials: credentials, settings: settings, renderer: renderer}, nil
}

func (s *Service) Render(ctx context.Context, token string, format Format) ([]byte, error) {
	if s == nil || s.credentials == nil || s.settings == nil {
		return nil, errors.New("subscription service is not initialized")
	}
	if format != FormatBase64 && format != FormatSingBox && format != FormatClash {
		return nil, errors.New("subscription format is invalid")
	}
	bundle, err := s.credentials.FindActiveBySubscriptionToken(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("load subscription credentials: %w", err)
	}
	settings, err := s.settings.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load subscription settings: %w", err)
	}
	switch format {
	case FormatBase64:
		value, err := s.renderer.RenderBase64(settings, bundle)
		return []byte(value), err
	case FormatSingBox:
		return s.renderer.RenderSingBox(settings, bundle)
	case FormatClash:
		return s.renderer.RenderClash(settings, bundle)
	}
	return nil, errors.New("subscription format is invalid")
}
