package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/secrets"
)

type CredentialSealer interface {
	Seal(telegramID int64, generation uint64, bundle domain.CredentialBundle) (secrets.SealedCredentialBundle, error)
}

type ProvisioningStore struct {
	transactions TransactionRunner
	issuer       domain.BundleIssuer
	sealer       CredentialSealer
}

func NewProvisioningStore(transactions TransactionRunner, issuer domain.BundleIssuer, sealer CredentialSealer) *ProvisioningStore {
	return &ProvisioningStore{transactions: transactions, issuer: issuer, sealer: sealer}
}

func (store *ProvisioningStore) Claim(ctx context.Context, telegramID int64, now time.Time) (domain.ProvisionedAccess, error) {
	return store.provision(ctx, telegramID, now, true, "vpn.claim", func(account *domain.AccessAccount) (domain.Issuance, error) {
		return account.Claim(now)
	})
}

func (store *ProvisioningStore) Approve(ctx context.Context, telegramID int64, now time.Time) (domain.ProvisionedAccess, error) {
	return store.provision(ctx, telegramID, now, true, "vpn.approve", func(account *domain.AccessAccount) (domain.Issuance, error) {
		return account.Approve(now)
	})
}

func (store *ProvisioningStore) Rotate(ctx context.Context, telegramID int64, now time.Time, resetPeriod bool) (domain.ProvisionedAccess, error) {
	return store.provision(ctx, telegramID, now, resetPeriod, "vpn.rotate", func(account *domain.AccessAccount) (domain.Issuance, error) {
		return account.Rotate(now, resetPeriod)
	})
}

func (store *ProvisioningStore) provision(
	ctx context.Context,
	telegramID int64,
	now time.Time,
	resetQuota bool,
	auditAction string,
	issue func(*domain.AccessAccount) (domain.Issuance, error),
) (domain.ProvisionedAccess, error) {
	if store == nil || store.transactions == nil || store.issuer == nil || store.sealer == nil {
		return domain.ProvisionedAccess{}, errors.New("provisioning dependencies are required")
	}
	if telegramID <= 0 {
		return domain.ProvisionedAccess{}, errors.New("Telegram ID must be positive")
	}
	if now.IsZero() {
		return domain.ProvisionedAccess{}, errors.New("issuance timestamp is required")
	}
	if issue == nil || auditAction == "" {
		return domain.ProvisionedAccess{}, errors.New("provisioning operation is required")
	}
	bundle, err := store.issuer.Issue()
	if err != nil {
		return domain.ProvisionedAccess{}, fmt.Errorf("issue credentials: %w", err)
	}

	var provisioned domain.ProvisionedAccess
	err = store.transactions.RunInTransaction(ctx, func(transaction Database) error {
		if transaction == nil {
			return errors.New("provisioning transaction is required")
		}
		snapshot, err := lockAccessSnapshot(ctx, transaction, telegramID)
		if err != nil {
			return err
		}
		account, err := domain.RestoreAccessAccount(snapshot)
		if err != nil {
			return fmt.Errorf("restore VPN user: %w", err)
		}
		issuance, err := issue(account)
		if err != nil {
			return err
		}
		sealed, err := store.sealer.Seal(telegramID, issuance.CredentialGeneration, bundle)
		if err != nil {
			return fmt.Errorf("seal credentials: %w", err)
		}
		if len(sealed.Nonce) != 12 || len(sealed.Ciphertext) <= 16 || sealed.SubscriptionTokenDigest == ([32]byte{}) {
			return errors.New("sealed credentials are invalid")
		}

		var quotaLimit, quotaPeriodSeconds int64
		if resetQuota {
			if err := transaction.QueryRow(ctx, `
				SELECT quota_limit_bytes, quota_period_seconds
				FROM qualification_settings
				WHERE singleton = TRUE`).Scan(&quotaLimit, &quotaPeriodSeconds); err != nil {
				return fmt.Errorf("load quota settings: %w", err)
			}
			if quotaLimit <= 0 || quotaPeriodSeconds <= 0 {
				return errors.New("persisted quota settings are invalid")
			}
		}

		updated := account.Snapshot()
		if _, err := transaction.Exec(ctx, `
			UPDATE vpn_users
			SET status = $1,
			    credential_generation = $2,
			    period_started_at = $3,
			    last_vpn_activity_at = $4,
			    updated_at = NOW()
			WHERE telegram_id = $5`,
			string(updated.Status), int64(updated.CredentialGeneration), updated.PeriodStartedAt,
			updated.LastVPNActivityAt, updated.TelegramID,
		); err != nil {
			return fmt.Errorf("persist provisioned VPN user: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO credential_bundles (
				telegram_id, generation, subscription_token_digest, nonce, ciphertext
			) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (telegram_id) DO UPDATE
			SET generation = EXCLUDED.generation,
			    subscription_token_digest = EXCLUDED.subscription_token_digest,
			    nonce = EXCLUDED.nonce,
			    ciphertext = EXCLUDED.ciphertext,
			    created_at = NOW()`,
			telegramID, int64(issuance.CredentialGeneration), sealed.SubscriptionTokenDigest[:],
			sealed.Nonce, sealed.Ciphertext,
		); err != nil {
			return fmt.Errorf("persist credential bundle: %w", err)
		}
		if resetQuota {
			if _, err := transaction.Exec(ctx, `
				INSERT INTO quota_windows (
					telegram_id, period_started_at, period_seconds, limit_bytes, used_bytes, blocked
				) VALUES ($1, $2, $3, $4, 0, FALSE)
				ON CONFLICT (telegram_id) DO UPDATE
				SET period_started_at = EXCLUDED.period_started_at,
				    period_seconds = EXCLUDED.period_seconds,
				    limit_bytes = EXCLUDED.limit_bytes,
				    used_bytes = 0,
				    blocked = FALSE,
				    updated_at = NOW()`,
				telegramID, issuance.PeriodStartedAt, quotaPeriodSeconds, quotaLimit,
			); err != nil {
				return fmt.Errorf("persist quota window: %w", err)
			}
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO core_action_outbox (telegram_id, action, available_at)
			VALUES ($1, 'reconcile', $2)
			ON CONFLICT (telegram_id, action) WHERE completed_at IS NULL DO NOTHING`,
			telegramID, now,
		); err != nil {
			return fmt.Errorf("queue core reconciliation: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO audit_events (action, target_type, target_id, details)
			VALUES ($1, 'vpn_user', $2, jsonb_build_object('generation', $3))`,
			auditAction, fmt.Sprint(telegramID), int64(issuance.CredentialGeneration),
		); err != nil {
			return fmt.Errorf("record VPN claim audit event: %w", err)
		}
		provisioned = domain.ProvisionedAccess{Issuance: issuance, Credentials: bundle}
		return nil
	})
	if err != nil {
		return domain.ProvisionedAccess{}, err
	}
	return provisioned, nil
}
