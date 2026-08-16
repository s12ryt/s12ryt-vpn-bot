package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/singbox"
)

var ErrCredentialBundleNotFound = errors.New("credential bundle not found")

type CredentialCryptor interface {
	SubscriptionTokenDigest(token string) ([32]byte, error)
	Open(telegramID int64, generation uint64, nonce, ciphertext []byte) (domain.CredentialBundle, error)
}

type CredentialStore struct {
	database Database
	cryptor  CredentialCryptor
}

func NewCredentialStore(database Database, cryptor CredentialCryptor) *CredentialStore {
	return &CredentialStore{database: database, cryptor: cryptor}
}

func (store *CredentialStore) FindActiveBySubscriptionToken(ctx context.Context, token string) (domain.CredentialBundle, error) {
	if store == nil || store.database == nil || store.cryptor == nil {
		return domain.CredentialBundle{}, errors.New("credential store dependencies are required")
	}
	digest, err := store.cryptor.SubscriptionTokenDigest(token)
	if err != nil {
		return domain.CredentialBundle{}, err
	}
	return store.openCredentialRow(store.database.QueryRow(ctx, `
		SELECT bundle.telegram_id, bundle.generation, bundle.nonce, bundle.ciphertext
		FROM credential_bundles AS bundle
		JOIN vpn_users AS vpn_user ON vpn_user.telegram_id = bundle.telegram_id
		WHERE bundle.subscription_token_digest = $1
		  AND vpn_user.status = 'active'
		  AND vpn_user.eligible = TRUE`, digest[:]))
}

func (store *CredentialStore) FindActiveByTelegramID(ctx context.Context, telegramID int64) (domain.CredentialBundle, error) {
	if store == nil || store.database == nil || store.cryptor == nil {
		return domain.CredentialBundle{}, errors.New("credential store dependencies are required")
	}
	if telegramID <= 0 {
		return domain.CredentialBundle{}, errors.New("Telegram ID must be positive")
	}
	return store.openCredentialRow(store.database.QueryRow(ctx, `
		SELECT bundle.telegram_id, bundle.generation, bundle.nonce, bundle.ciphertext
		FROM credential_bundles AS bundle
		JOIN vpn_users AS vpn_user ON vpn_user.telegram_id = bundle.telegram_id
		WHERE bundle.telegram_id = $1
		  AND vpn_user.status = 'active'
		  AND vpn_user.eligible = TRUE`, telegramID))
}

func (store *CredentialStore) ListActive(ctx context.Context) ([]singbox.User, error) {
	if store == nil || store.database == nil || store.cryptor == nil {
		return nil, errors.New("credential store dependencies are required")
	}
	var telegramIDs, generations []int64
	var nonces, ciphertexts [][]byte
	if err := store.database.QueryRow(ctx, `
		SELECT
			COALESCE(array_agg(bundle.telegram_id ORDER BY bundle.telegram_id), ARRAY[]::BIGINT[]),
			COALESCE(array_agg(bundle.generation ORDER BY bundle.telegram_id), ARRAY[]::BIGINT[]),
			COALESCE(array_agg(bundle.nonce ORDER BY bundle.telegram_id), ARRAY[]::BYTEA[]),
			COALESCE(array_agg(bundle.ciphertext ORDER BY bundle.telegram_id), ARRAY[]::BYTEA[])
		FROM credential_bundles AS bundle
		JOIN vpn_users AS vpn_user ON vpn_user.telegram_id = bundle.telegram_id
		JOIN quota_windows AS quota ON quota.telegram_id = bundle.telegram_id
		CROSS JOIN traffic_health AS health
		WHERE vpn_user.status = 'active'
		  AND vpn_user.eligible = TRUE
		  AND quota.blocked = FALSE
		  AND health.singleton = TRUE
		  AND health.fail_closed = FALSE`).Scan(&telegramIDs, &generations, &nonces, &ciphertexts); err != nil {
		return nil, fmt.Errorf("list active credential bundles: %w", err)
	}
	if len(telegramIDs) != len(generations) || len(telegramIDs) != len(nonces) || len(telegramIDs) != len(ciphertexts) {
		return nil, errors.New("persisted credential list is inconsistent")
	}
	for index, telegramID := range telegramIDs {
		if telegramID <= 0 || generations[index] <= 0 || (index > 0 && telegramIDs[index-1] >= telegramID) {
			return nil, errors.New("persisted credential list is invalid")
		}
	}

	users := make([]singbox.User, len(telegramIDs))
	for index, telegramID := range telegramIDs {
		bundle, err := store.cryptor.Open(telegramID, uint64(generations[index]), nonces[index], ciphertexts[index])
		if err != nil {
			return nil, fmt.Errorf("decrypt credential bundle for Telegram ID %d: %w", telegramID, err)
		}
		users[index] = singbox.User{TelegramID: telegramID, Credentials: bundle}
	}
	return users, nil
}

func (store *CredentialStore) openCredentialRow(row pgx.Row) (domain.CredentialBundle, error) {
	var telegramID, generation int64
	var nonce, ciphertext []byte
	err := row.Scan(&telegramID, &generation, &nonce, &ciphertext)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CredentialBundle{}, ErrCredentialBundleNotFound
	}
	if err != nil {
		return domain.CredentialBundle{}, fmt.Errorf("load credential bundle: %w", err)
	}
	if telegramID <= 0 || generation <= 0 {
		return domain.CredentialBundle{}, errors.New("persisted credential owner is invalid")
	}
	bundle, err := store.cryptor.Open(telegramID, uint64(generation), nonce, ciphertext)
	if err != nil {
		return domain.CredentialBundle{}, fmt.Errorf("decrypt credential bundle: %w", err)
	}
	return bundle, nil
}
