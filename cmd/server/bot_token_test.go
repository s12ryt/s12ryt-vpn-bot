package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/httpapi"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/postgres"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
)

func botTokenIdentity() telegram.User {
	return telegram.User{ID: 42, IsBot: true, Username: "member_bot"}
}

func TestBotTokenManagerRotatesVerifyPersistThenSwap(t *testing.T) {
	now := time.Date(2026, 8, 17, 13, 0, 0, 0, time.UTC)
	identity := botTokenIdentity()
	wrapper, err := telegram.NewSwapAwareClient(&telegram.Client{}, identity)
	if err != nil {
		t.Fatalf("wrapper: %v", err)
	}
	before := wrapper.Current()
	store := &botSettingsStoreStub{}
	factory := func(token string) *telegram.Client { return &telegram.Client{} }
	manager, err := buildBotTokenManager(botTokenDependencies{
		wrapper: wrapper, store: store,
		factory: factory, verify: func(_ context.Context, _ *telegram.Client) (telegram.User, error) { return identity, nil },
	})
	if err != nil {
		t.Fatalf("buildBotTokenManager() error = %v", err)
	}

	if err := manager.Rotate(context.Background(), 9001, "rotated-token", now); err != nil {
		t.Fatalf("Rotate() error = %v", err)
	}
	if !store.saveCalled || store.saveActor != 9001 || store.saveToken != "rotated-token" || store.saveUsername != "member_bot" {
		t.Fatalf("store = %+v", store)
	}
	if wrapper.Current() == before {
		t.Fatal("wrapper must use the new client after successful rotation")
	}
}

func TestBotTokenManagerRejectsVerificationFailureWithoutSideEffects(t *testing.T) {
	wrapper, _ := telegram.NewSwapAwareClient(&telegram.Client{}, botTokenIdentity())
	before := wrapper.Current()
	store := &botSettingsStoreStub{}
	manager, _ := buildBotTokenManager(botTokenDependencies{
		wrapper: wrapper, store: store,
		factory: func(string) *telegram.Client { return &telegram.Client{} },
		verify: func(_ context.Context, _ *telegram.Client) (telegram.User, error) {
			return telegram.User{}, errors.New("telegram down")
		},
	})

	err := manager.Rotate(context.Background(), 9001, "rotated-token", time.Now().UTC())
	if !errors.Is(err, telegram.ErrBotVerificationFailed) {
		t.Fatalf("Rotate() error = %v, want ErrBotVerificationFailed", err)
	}
	if store.saveCalled || wrapper.Current() != before {
		t.Fatalf("storeCalled=%v swapped=%v", store.saveCalled, wrapper.Current() != before)
	}
}

func TestBotTokenManagerRejectsDifferentBotIdentity(t *testing.T) {
	wrapper, _ := telegram.NewSwapAwareClient(&telegram.Client{}, botTokenIdentity())
	store := &botSettingsStoreStub{}
	other := telegram.User{ID: 99, IsBot: true, Username: "other_bot"}
	manager, _ := buildBotTokenManager(botTokenDependencies{
		wrapper: wrapper, store: store,
		factory: func(string) *telegram.Client { return &telegram.Client{} },
		verify:  func(_ context.Context, _ *telegram.Client) (telegram.User, error) { return other, nil },
	})

	err := manager.Rotate(context.Background(), 9001, "rotated-token", time.Now().UTC())
	if !errors.Is(err, telegram.ErrBotIdentityChanged) {
		t.Fatalf("Rotate() error = %v, want ErrBotIdentityChanged", err)
	}
	if store.saveCalled {
		t.Fatal("different bot must not be persisted")
	}
}

func TestBotTokenManagerDoesNotSwapWhenPersistFails(t *testing.T) {
	identity := botTokenIdentity()
	wrapper, _ := telegram.NewSwapAwareClient(&telegram.Client{}, identity)
	before := wrapper.Current()
	store := &botSettingsStoreStub{saveErr: errors.New("database unavailable")}
	manager, _ := buildBotTokenManager(botTokenDependencies{
		wrapper: wrapper, store: store,
		factory: func(string) *telegram.Client { return &telegram.Client{} },
		verify:  func(_ context.Context, _ *telegram.Client) (telegram.User, error) { return identity, nil },
	})

	if err := manager.Rotate(context.Background(), 9001, "rotated-token", time.Now().UTC()); err == nil {
		t.Fatal("persist failure must propagate")
	}
	if wrapper.Current() != before {
		t.Fatal("wrapper must keep the verified live client when persistence fails")
	}
}

func TestBotTokenManagerMapsOverview(t *testing.T) {
	identity := botTokenIdentity()
	wrapper, _ := telegram.NewSwapAwareClient(&telegram.Client{}, identity)
	updatedAt := time.Date(2026, 8, 17, 13, 0, 0, 0, time.UTC)
	store := &botSettingsStoreStub{overview: postgres.BotSettingsOverview{BotUsername: "member_bot", UpdatedAt: updatedAt}}
	manager, _ := buildBotTokenManager(botTokenDependencies{
		wrapper: wrapper, store: store,
		factory: func(string) *telegram.Client { return &telegram.Client{} },
		verify: func(_ context.Context, _ *telegram.Client) (telegram.User, error) {
			return identity, nil
		},
	})

	overview, err := manager.GetOverview(context.Background())
	if err != nil {
		t.Fatalf("GetOverview() error = %v", err)
	}
	if overview.BotUsername != "member_bot" || !overview.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("overview = %#v", overview)
	}
	var _ httpapi.BotSettingsManager = manager
}

func TestBuildBotTokenManagerRejectsMissingDependencies(t *testing.T) {
	identity := botTokenIdentity()
	wrapper, _ := telegram.NewSwapAwareClient(&telegram.Client{}, identity)
	if _, err := buildBotTokenManager(botTokenDependencies{}); err == nil {
		t.Fatal("empty dependencies must be rejected")
	}
	if _, err := buildBotTokenManager(botTokenDependencies{store: &botSettingsStoreStub{}}); err == nil {
		t.Fatal("missing wrapper must be rejected")
	}
	if _, err := buildBotTokenManager(botTokenDependencies{wrapper: wrapper}); err == nil {
		t.Fatal("missing store must be rejected")
	}
}

type botSettingsStoreStub struct {
	overview     postgres.BotSettingsOverview
	saveCalled   bool
	saveActor    int64
	saveToken    string
	saveUsername string
	saveErr      error
}

func (stub *botSettingsStoreStub) Save(_ context.Context, actor int64, token, username string, _ time.Time) error {
	stub.saveCalled, stub.saveActor, stub.saveToken, stub.saveUsername = true, actor, token, username
	return stub.saveErr
}

func (stub *botSettingsStoreStub) Overview(context.Context) (postgres.BotSettingsOverview, error) {
	return stub.overview, nil
}
