package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/secrets"
)

const botTokenPurpose = "telegram/bot-token"

// ErrBotTokenNotConfigured reports that no bot token has been persisted yet;
// callers fall back to the bootstrap environment value.
var ErrBotTokenNotConfigured = errors.New("bot token is not configured")

type BotSettingsOverview struct {
	BotUsername string    `json:"bot_username"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// BotSettingsStore persists the owner-rotated bot token as AEAD ciphertext.
type BotSettingsStore struct {
	transactions TransactionRunner
	database     Database
	cipher       SecretValueCipher
}

func NewBotSettingsStore(transactions TransactionRunner, database Database, cipher SecretValueCipher) *BotSettingsStore {
	return &BotSettingsStore{transactions: transactions, database: database, cipher: cipher}
}

func (store *BotSettingsStore) Save(ctx context.Context, actorTelegramID int64, token, botUsername string, now time.Time) error {
	if store == nil || store.transactions == nil || store.cipher == nil {
		return errors.New("bot settings store dependencies are required")
	}
	token = strings.TrimSpace(token)
	botUsername = strings.TrimSpace(botUsername)
	if actorTelegramID <= 0 || token == "" || botUsername == "" || now.IsZero() {
		return errors.New("bot token update parameters are invalid")
	}
	sealed, err := store.cipher.Seal(botTokenPurpose, token)
	if err != nil {
		return fmt.Errorf("seal bot token: %w", err)
	}
	return store.transactions.RunInTransaction(ctx, func(transaction Database) error {
		if _, err := transaction.Exec(ctx, `
			UPDATE bot_settings
			SET bot_username = $1,
			    token_nonce = $2,
			    token_ciphertext = $3,
			    updated_at = $4
			WHERE singleton = TRUE`,
			botUsername, sealed.Nonce, sealed.Ciphertext, now,
		); err != nil {
			return fmt.Errorf("update bot settings: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO audit_events (actor_telegram_id, action, target_type, target_id, details, created_at)
			VALUES ($1, 'bot.token.update', 'bot', '', jsonb_build_object('bot_username', $2), $3)`,
			actorTelegramID, botUsername, now,
		); err != nil {
			return fmt.Errorf("audit bot token update: %w", err)
		}
		return nil
	})
}

// Token returns the decrypted bot token for startup preference over the
// bootstrap environment value.
func (store *BotSettingsStore) Token(ctx context.Context) (string, error) {
	if store == nil || store.database == nil || store.cipher == nil {
		return "", errors.New("bot settings store dependencies are required")
	}
	var nonce, ciphertext []byte
	if err := store.database.QueryRow(ctx, `
		SELECT token_nonce, token_ciphertext
		FROM bot_settings
		WHERE singleton = TRUE`).Scan(&nonce, &ciphertext); err != nil {
		return "", fmt.Errorf("read bot settings: %w", err)
	}
	if len(nonce) == 0 || len(ciphertext) == 0 {
		return "", ErrBotTokenNotConfigured
	}
	token, err := store.cipher.Open(botTokenPurpose, secrets.SealedValue{Nonce: nonce, Ciphertext: ciphertext})
	if err != nil {
		return "", fmt.Errorf("open bot token: %w", err)
	}
	if token == "" {
		return "", ErrBotTokenNotConfigured
	}
	return token, nil
}

// Overview exposes only non-secret bot settings; the query never touches the
// encrypted token columns.
func (store *BotSettingsStore) Overview(ctx context.Context) (BotSettingsOverview, error) {
	if store == nil || store.database == nil {
		return BotSettingsOverview{}, errors.New("bot settings database is required")
	}
	var result BotSettingsOverview
	var updatedAt sql.NullTime
	if err := store.database.QueryRow(ctx, `
		SELECT bot_username, updated_at
		FROM bot_settings
		WHERE singleton = TRUE`).Scan(&result.BotUsername, &updatedAt); err != nil {
		return BotSettingsOverview{}, fmt.Errorf("read bot settings overview: %w", err)
	}
	if result.BotUsername == "" {
		result.BotUsername = "尚未設定"
	}
	if updatedAt.Valid {
		result.UpdatedAt = updatedAt.Time.UTC()
	}
	return result, nil
}
