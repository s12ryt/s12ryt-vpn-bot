package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/acme"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

const duckDNSTokenPurpose = "acme/duckdns-token"

// TLSSettingsStore persists the ACME/TLS configuration. The DuckDNS token is
// stored as AEAD ciphertext only; issuance state transitions queue core
// reconciliation and write actor-audited events.
type TLSSettingsStore struct {
	transactions TransactionRunner
	database     Database
	cipher       SecretValueCipher
}

func NewTLSSettingsStore(transactions TransactionRunner, database Database, cipher SecretValueCipher) *TLSSettingsStore {
	return &TLSSettingsStore{transactions: transactions, database: database, cipher: cipher}
}

func (store *TLSSettingsStore) Save(ctx context.Context, actorTelegramID int64, update domain.TLSSettingsUpdate, now time.Time) error {
	if store == nil || store.transactions == nil || store.cipher == nil {
		return errors.New("TLS settings store dependencies are required")
	}
	if actorTelegramID <= 0 || now.IsZero() {
		return errors.New("TLS settings update parameters are invalid")
	}
	validated, err := acme.ValidateSettings(acme.Settings{
		Mode:             acme.Mode(update.Mode),
		Domain:           update.Domain,
		Challenge:        acme.Challenge(update.Challenge),
		Email:            update.Email,
		CADirectoryURLs:  update.CADirectoryURLs,
		TermsAccepted:    update.TermsAccepted,
		DNSProviderName:  "duckdns",
		DNSProviderToken: strings.TrimSpace(update.DuckDNSToken),
	})
	if err != nil {
		return fmt.Errorf("validate TLS settings: %w", err)
	}
	return store.transactions.RunInTransaction(ctx, func(transaction Database) error {
		var nonce, ciphertext []byte
		if err := transaction.QueryRow(ctx, `
			SELECT duckdns_token_nonce, duckdns_token_ciphertext
			FROM tls_settings
			WHERE singleton = TRUE
			FOR UPDATE`).Scan(&nonce, &ciphertext); err != nil {
			return fmt.Errorf("lock TLS settings: %w", err)
		}
		if validated.Mode == acme.ModeDuckDNS {
			if validated.DNSProviderToken != "" {
				sealed, sealErr := store.cipher.Seal(duckDNSTokenPurpose, validated.DNSProviderToken)
				if sealErr != nil {
					return fmt.Errorf("seal DuckDNS token: %w", sealErr)
				}
				nonce, ciphertext = sealed.Nonce, sealed.Ciphertext
			} else if len(nonce) == 0 || len(ciphertext) == 0 {
				return errors.New("DuckDNS token is required for duckdns mode")
			}
		} else {
			nonce, ciphertext = nil, nil
		}
		if _, err := transaction.Exec(ctx, `
			UPDATE tls_settings
			SET configured = TRUE,
			    mode = $1, domain = $2, challenge = $3, email = $4,
			    ca_directory_urls = $5, terms_accepted = $6,
			    duckdns_token_nonce = $7, duckdns_token_ciphertext = $8,
			    updated_at = $9
			WHERE singleton = TRUE`,
			string(validated.Mode), validated.Domain, string(validated.Challenge), validated.Email,
			validated.CADirectoryURLs, validated.TermsAccepted, nonce, ciphertext, now,
		); err != nil {
			return fmt.Errorf("update TLS settings: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO audit_events (actor_telegram_id, action, target_type, target_id, details, created_at)
			VALUES ($1, 'tls.settings.update', 'tls', '', jsonb_build_object(
				'mode', $2, 'domain', $3, 'challenge', $4, 'ca_count', $5
			), $6)`,
			actorTelegramID, string(validated.Mode), validated.Domain, string(validated.Challenge), len(validated.CADirectoryURLs), now,
		); err != nil {
			return fmt.Errorf("audit TLS settings update: %w", err)
		}
		return nil
	})
}

func (store *TLSSettingsStore) GetOverview(ctx context.Context) (domain.TLSSettingsOverview, error) {
	if store == nil || store.database == nil {
		return domain.TLSSettingsOverview{}, errors.New("TLS settings database is required")
	}
	var result domain.TLSSettingsOverview
	var expires sql.NullTime
	err := store.database.QueryRow(ctx, `
		SELECT configured, mode, domain, challenge, email, ca_directory_urls,
		       terms_accepted, duckdns_token_nonce IS NOT NULL AS has_duckdns_token,
		       state, certificate_expires_at, last_issued_ca
		FROM tls_settings
		WHERE singleton = TRUE`).Scan(
		&result.Configured, &result.Mode, &result.Domain, &result.Challenge, &result.Email,
		&result.CADirectoryURLs, &result.TermsAccepted, &result.HasDuckDNSToken,
		&result.State, &expires, &result.LastIssuedCA,
	)
	if err != nil {
		return domain.TLSSettingsOverview{}, fmt.Errorf("read TLS settings overview: %w", err)
	}
	switch result.Mode {
	case string(acme.ModeSSLIP), string(acme.ModeDuckDNS), string(acme.ModeCustom):
	default:
		return domain.TLSSettingsOverview{}, errors.New("persisted TLS mode is invalid")
	}
	switch result.State {
	case "unissued", "issued", "failed":
	default:
		return domain.TLSSettingsOverview{}, errors.New("persisted TLS state is invalid")
	}
	if expires.Valid {
		result.CertificateExpiresAt = expires.Time.UTC()
	}
	return result, nil
}

func (store *TLSSettingsStore) RecordIssuance(ctx context.Context, caDirectoryURL string, expiresAt, now time.Time) error {
	if store == nil || store.transactions == nil {
		return errors.New("TLS settings store dependencies are required")
	}
	if now.IsZero() || !expiresAt.After(now) {
		return errors.New("certificate expiry must be in the future")
	}
	parsed, err := url.Parse(strings.TrimSpace(caDirectoryURL))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return errors.New("issuing CA directory URL is invalid")
	}
	return store.transactions.RunInTransaction(ctx, func(transaction Database) error {
		if _, err := transaction.Exec(ctx, `
			UPDATE tls_settings
			SET state = 'issued', certificate_expires_at = $1, last_issued_ca = $2,
			    last_failure_at = NULL, last_failure_reason = '', updated_at = $3
			WHERE singleton = TRUE`, expiresAt, parsed.String(), now); err != nil {
			return fmt.Errorf("record TLS issuance: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO core_action_outbox (telegram_id, action, available_at)
			SELECT vpn_user.telegram_id, 'reconcile', $1
			FROM vpn_users AS vpn_user
			WHERE vpn_user.status = 'active' AND vpn_user.eligible = TRUE
			ON CONFLICT (telegram_id, action) WHERE completed_at IS NULL DO NOTHING`, now); err != nil {
			return fmt.Errorf("queue TLS reconciliation: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO audit_events (actor_telegram_id, action, target_type, target_id, details, created_at)
			VALUES (NULL, 'tls.certificate.issued', 'tls', '', jsonb_build_object(
				'ca', $1, 'expires_at', $2
			), $3)`, parsed.String(), expiresAt, now); err != nil {
			return fmt.Errorf("audit TLS issuance: %w", err)
		}
		return nil
	})
}

func (store *TLSSettingsStore) RecordFailure(ctx context.Context, reason string, now time.Time) error {
	if store == nil || store.transactions == nil {
		return errors.New("TLS settings store dependencies are required")
	}
	if now.IsZero() {
		return errors.New("failure timestamp is required")
	}
	switch reason {
	case "all_cas_failed":
	default:
		return errors.New("unknown TLS failure reason")
	}
	return store.transactions.RunInTransaction(ctx, func(transaction Database) error {
		if _, err := transaction.Exec(ctx, `
			UPDATE tls_settings
			SET last_failure_at = $1, last_failure_reason = $2,
			    state = CASE WHEN state = 'issued' AND certificate_expires_at > $1 THEN 'issued' ELSE 'failed' END,
			    updated_at = $1
			WHERE singleton = TRUE`, now, reason); err != nil {
			return fmt.Errorf("record TLS failure: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO audit_events (actor_telegram_id, action, target_type, target_id, details, created_at)
			VALUES (NULL, 'tls.certificate.failed', 'tls', '', jsonb_build_object('reason', $1), $2)`,
			reason, now); err != nil {
			return fmt.Errorf("audit TLS failure: %w", err)
		}
		return nil
	})
}
