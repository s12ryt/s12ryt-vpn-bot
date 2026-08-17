package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/httpapi"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/postgres"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
)

type botSettingsStore interface {
	Save(context.Context, int64, string, string, time.Time) error
	Overview(context.Context) (postgres.BotSettingsOverview, error)
}

type botTokenDependencies struct {
	wrapper *telegram.SwapAwareClient
	store   botSettingsStore
	factory telegram.ClientFactory
	verify  telegram.IdentityVerifier
}

// botTokenManager rotates the bot token safely: verify the candidate controls
// the same bot, persist the AEAD-sealed token, and only then swap the live
// client so every component starts using the new token without a restart.
type botTokenManager struct {
	wrapper *telegram.SwapAwareClient
	store   botSettingsStore
	factory telegram.ClientFactory
	verify  telegram.IdentityVerifier
}

func buildBotTokenManager(dependencies botTokenDependencies) (*botTokenManager, error) {
	if dependencies.wrapper == nil || dependencies.store == nil || dependencies.factory == nil || dependencies.verify == nil {
		return nil, errors.New("bot token manager dependencies are required")
	}
	return &botTokenManager{
		wrapper: dependencies.wrapper,
		store:   dependencies.store,
		factory: dependencies.factory,
		verify:  dependencies.verify,
	}, nil
}

func (manager *botTokenManager) Rotate(ctx context.Context, actorTelegramID int64, token string, now time.Time) error {
	if manager == nil || manager.wrapper == nil || manager.store == nil {
		return errors.New("bot token manager is not initialized")
	}
	identity, err := manager.verify(ctx, manager.factory(token))
	if err != nil {
		return fmt.Errorf("%w: %v", telegram.ErrBotVerificationFailed, err)
	}
	expected := manager.wrapper.Identity()
	if identity.ID != expected.ID || !identity.IsBot || identity.Username == "" {
		return telegram.ErrBotIdentityChanged
	}
	if err := manager.store.Save(ctx, actorTelegramID, token, identity.Username, now); err != nil {
		return fmt.Errorf("persist bot token: %w", err)
	}
	return manager.wrapper.Swap(ctx, token, manager.factory, func(context.Context, *telegram.Client) (telegram.User, error) {
		return identity, nil
	})
}

func (manager *botTokenManager) GetOverview(ctx context.Context) (httpapi.BotSettingsOverview, error) {
	if manager == nil || manager.store == nil {
		return httpapi.BotSettingsOverview{}, errors.New("bot token manager is not initialized")
	}
	overview, err := manager.store.Overview(ctx)
	if err != nil {
		return httpapi.BotSettingsOverview{}, err
	}
	return httpapi.BotSettingsOverview{BotUsername: overview.BotUsername, UpdatedAt: overview.UpdatedAt}, nil
}
