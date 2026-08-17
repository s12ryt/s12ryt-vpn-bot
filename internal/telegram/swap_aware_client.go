package telegram

import (
	"context"
	"errors"
	"sync"
)

// ClientFactory builds a client for a candidate token during a hot swap.
type ClientFactory func(token string) *Client

// IdentityVerifier confirms a candidate client controls the expected bot.
// Production code passes a closure calling GetMe.
type IdentityVerifier func(context.Context, *Client) (User, error)

// ErrBotIdentityChanged reports that the candidate token belongs to a
// different bot or an invalid identity; a hot swap is refused so owners must
// rebootstrap deliberately instead of silently repointing the deployment.
var ErrBotIdentityChanged = errors.New("bot identity changed")

// ErrBotVerificationFailed reports that the candidate token could not be
// verified against the Telegram API before any state changed.
var ErrBotVerificationFailed = errors.New("bot token verification failed")

// SwapAwareClient atomically delegates every Telegram call to the current
// client. Rotating the bot token replaces the underlying client without
// restarting any component holding this wrapper.
type SwapAwareClient struct {
	mu       sync.Mutex
	current  *Client
	identity User
}

func NewSwapAwareClient(initial *Client, identity User) (*SwapAwareClient, error) {
	if initial == nil || identity.ID <= 0 || !identity.IsBot || identity.Username == "" {
		return nil, errors.New("swap-aware client requires an initial client and verified bot identity")
	}
	return &SwapAwareClient{current: initial, identity: identity}, nil
}

// Identity returns the verified startup identity; it never changes because a
// hot swap is only accepted for the same bot.
func (wrapper *SwapAwareClient) Identity() User {
	return wrapper.identity
}

// Current exposes the active client for startup wiring only.
func (wrapper *SwapAwareClient) Current() *Client {
	wrapper.mu.Lock()
	defer wrapper.mu.Unlock()
	return wrapper.current
}

// Swap validates the candidate token controls the same bot and atomically
// installs the new client. Any failure leaves the previous client active.
func (wrapper *SwapAwareClient) Swap(ctx context.Context, token string, factory ClientFactory, verify IdentityVerifier) error {
	if wrapper == nil || token == "" || factory == nil || verify == nil {
		return errors.New("token swap requires a token, factory and verifier")
	}
	candidate := factory(token)
	if candidate == nil {
		return errors.New("token swap factory returned no client")
	}
	verified, err := verify(ctx, candidate)
	if err != nil {
		return err
	}
	if verified.ID != wrapper.identity.ID || !verified.IsBot || verified.Username == "" {
		return ErrBotIdentityChanged
	}
	wrapper.mu.Lock()
	wrapper.current = candidate
	wrapper.identity.Username = verified.Username
	wrapper.mu.Unlock()
	return nil
}

func (wrapper *SwapAwareClient) GetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	return wrapper.Current().GetUpdates(ctx, offset)
}

func (wrapper *SwapAwareClient) SendMessage(ctx context.Context, chatID int64, text string) error {
	return wrapper.Current().SendMessage(ctx, chatID, text)
}

func (wrapper *SwapAwareClient) SendPhoto(ctx context.Context, chatID int64, caption string, png []byte) error {
	return wrapper.Current().SendPhoto(ctx, chatID, caption, png)
}

func (wrapper *SwapAwareClient) SendApprovalRequest(ctx context.Context, administratorID, targetTelegramID int64) error {
	return wrapper.Current().SendApprovalRequest(ctx, administratorID, targetTelegramID)
}

func (wrapper *SwapAwareClient) AnswerCallbackQuery(ctx context.Context, callbackID, text string) error {
	return wrapper.Current().AnswerCallbackQuery(ctx, callbackID, text)
}

func (wrapper *SwapAwareClient) GetChatMember(ctx context.Context, chatID, userID int64) (ChatMember, error) {
	return wrapper.Current().GetChatMember(ctx, chatID, userID)
}

func (wrapper *SwapAwareClient) GetMe(ctx context.Context) (User, error) {
	return wrapper.Current().GetMe(ctx)
}
