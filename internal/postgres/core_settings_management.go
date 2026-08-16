package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/secrets"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/singbox"
)

type SecretValueCipher interface {
	Seal(purpose, value string) (secrets.SealedValue, error)
	Open(purpose string, sealed secrets.SealedValue) (string, error)
}

type CoreSettingsManagementStore struct {
	transactions TransactionRunner
	database     Database
	cipher       SecretValueCipher
}

func NewCoreSettingsManagementStore(transactions TransactionRunner, database Database, cipher SecretValueCipher) *CoreSettingsManagementStore {
	return &CoreSettingsManagementStore{transactions: transactions, database: database, cipher: cipher}
}

func (store *CoreSettingsManagementStore) GetCore(ctx context.Context) (domain.CoreSettingsOverview, error) {
	if store == nil || store.database == nil {
		return domain.CoreSettingsOverview{}, errors.New("core settings database is required")
	}
	var result domain.CoreSettingsOverview
	var vlessPort, hysteria2Port, tuicPort, anyTLSPort, realityPort int
	err := store.database.QueryRow(ctx, `
		SELECT configured,
		       COALESCE(host(listen_ipv4), ''),
		       COALESCE(host(listen_ipv6), ''),
		       vless_port, hysteria2_port, tuic_port, anytls_port,
		       tls_server_name, tls_certificate_path, tls_key_path,
		       reality_server, reality_server_port, reality_short_id,
		       stats_listen, allow_ipv4_outbound,
		       configured AS has_reality_private_key
		FROM core_settings
		WHERE singleton = TRUE`).Scan(
		&result.Configured, &result.ListenIPv4, &result.ListenIPv6,
		&vlessPort, &hysteria2Port, &tuicPort, &anyTLSPort,
		&result.TLSServerName, &result.TLSCertificatePath, &result.TLSKeyPath,
		&result.RealityServer, &realityPort, &result.RealityShortID,
		&result.StatsListen, &result.AllowIPv4Outbound, &result.HasRealityPrivateKey,
	)
	if err != nil {
		return domain.CoreSettingsOverview{}, fmt.Errorf("read core settings overview: %w", err)
	}
	if !validPort(vlessPort) || !validPort(hysteria2Port) || !validPort(tuicPort) || !validPort(anyTLSPort) || !validPort(realityPort) {
		return domain.CoreSettingsOverview{}, errors.New("persisted core settings port is invalid")
	}
	result.VLESSPort = uint16(vlessPort)
	result.Hysteria2Port = uint16(hysteria2Port)
	result.TUICPort = uint16(tuicPort)
	result.AnyTLSPort = uint16(anyTLSPort)
	result.RealityServerPort = uint16(realityPort)
	return result, nil
}

func (store *CoreSettingsManagementStore) UpdateCore(ctx context.Context, actorTelegramID int64, input domain.CoreSettingsUpdate, now time.Time) error {
	if store == nil || store.transactions == nil || store.cipher == nil {
		return errors.New("core settings management dependencies are required")
	}
	if actorTelegramID <= 0 || now.IsZero() || !input.Configured {
		return errors.New("core settings update parameters are invalid")
	}
	validationKey := input.RealityPrivateKey
	if strings.TrimSpace(validationKey) == "" {
		validationKey = base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	}
	if err := validateCoreSettingsUpdate(input, validationKey); err != nil {
		return err
	}

	return store.transactions.RunInTransaction(ctx, func(transaction Database) error {
		var nonce, ciphertext []byte
		if err := transaction.QueryRow(ctx, `
			SELECT reality_private_key_nonce, reality_private_key_ciphertext
			FROM core_settings
			WHERE singleton = TRUE
			FOR UPDATE`).Scan(&nonce, &ciphertext); err != nil {
			return fmt.Errorf("lock core settings: %w", err)
		}

		privateKey := input.RealityPrivateKey
		if strings.TrimSpace(privateKey) == "" {
			if len(nonce) == 0 || len(ciphertext) == 0 {
				return errors.New("REALITY private key is required for initial configuration")
			}
			opened, err := store.cipher.Open(realityPrivateKeyPurpose, secrets.SealedValue{Nonce: nonce, Ciphertext: ciphertext})
			if err != nil {
				return fmt.Errorf("open existing REALITY private key: %w", err)
			}
			privateKey = opened
		} else {
			sealed, err := store.cipher.Seal(realityPrivateKeyPurpose, privateKey)
			if err != nil {
				return fmt.Errorf("seal REALITY private key: %w", err)
			}
			nonce, ciphertext = sealed.Nonce, sealed.Ciphertext
		}
		if err := validateCoreSettingsUpdate(input, privateKey); err != nil {
			return err
		}

		if _, err := transaction.Exec(ctx, `
			UPDATE core_settings
			SET configured = TRUE,
			    listen_ipv4 = NULLIF($1, '')::INET,
			    listen_ipv6 = NULLIF($2, '')::INET,
			    vless_port = $3, hysteria2_port = $4, tuic_port = $5, anytls_port = $6,
			    tls_server_name = $7, tls_certificate_path = $8, tls_key_path = $9,
			    reality_server = $10, reality_server_port = $11,
			    reality_private_key_nonce = $12, reality_private_key_ciphertext = $13,
			    reality_short_id = $14, stats_listen = $15, allow_ipv4_outbound = $16,
			    updated_at = $17
			WHERE singleton = TRUE`,
			input.ListenIPv4, input.ListenIPv6, int(input.VLESSPort), int(input.Hysteria2Port), int(input.TUICPort), int(input.AnyTLSPort),
			strings.TrimSpace(input.TLSServerName), input.TLSCertificatePath, input.TLSKeyPath,
			strings.TrimSpace(input.RealityServer), int(input.RealityServerPort), nonce, ciphertext,
			input.RealityShortID, input.StatsListen, input.AllowIPv4Outbound, now,
		); err != nil {
			return fmt.Errorf("update core settings: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO core_action_outbox (telegram_id, action, available_at)
			SELECT vpn_user.telegram_id, 'reconcile', $1
			FROM vpn_users AS vpn_user
			WHERE vpn_user.status = 'active' AND vpn_user.eligible = TRUE
			ON CONFLICT (telegram_id, action) WHERE completed_at IS NULL DO NOTHING`, now); err != nil {
			return fmt.Errorf("queue core settings reconciliation: %w", err)
		}
		if _, err := transaction.Exec(ctx, `
			INSERT INTO audit_events (actor_telegram_id, action, target_type, target_id, details, created_at)
			VALUES ($1, 'core.settings.update', 'core', '', jsonb_build_object(
				'listen_ipv4', $2, 'listen_ipv6', $3, 'allow_ipv4_outbound', $4,
				'vless_port', $5, 'hysteria2_port', $6, 'tuic_port', $7, 'anytls_port', $8
			), $9)`, actorTelegramID, input.ListenIPv4, input.ListenIPv6, input.AllowIPv4Outbound,
			int(input.VLESSPort), int(input.Hysteria2Port), int(input.TUICPort), int(input.AnyTLSPort), now); err != nil {
			return fmt.Errorf("audit core settings update: %w", err)
		}
		return nil
	})
}

func validateCoreSettingsUpdate(input domain.CoreSettingsUpdate, privateKey string) error {
	settings := singbox.Settings{
		ListenIPv4: input.ListenIPv4, ListenIPv6: input.ListenIPv6,
		VLESSPort: input.VLESSPort, Hysteria2Port: input.Hysteria2Port, TUICPort: input.TUICPort, AnyTLSPort: input.AnyTLSPort,
		TLSServerName: input.TLSServerName, TLSCertificatePath: input.TLSCertificatePath, TLSKeyPath: input.TLSKeyPath,
		RealityServer: input.RealityServer, RealityServerPort: input.RealityServerPort,
		RealityPrivateKey: privateKey, RealityShortID: input.RealityShortID,
		StatsListen: input.StatsListen, AllowIPv4Outbound: input.AllowIPv4Outbound,
	}
	if _, err := (singbox.Generator{}).Generate(settings); err != nil {
		return fmt.Errorf("validate core settings: %w", err)
	}
	return nil
}
