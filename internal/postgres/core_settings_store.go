package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/secrets"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/singbox"
)

var ErrCoreNotConfigured = errors.New("sing-box core is not configured")

const realityPrivateKeyPurpose = "sing-box/reality-private-key"

type SecretValueOpener interface {
	Open(purpose string, sealed secrets.SealedValue) (string, error)
}

type CoreSettingsStore struct {
	database Database
	opener   SecretValueOpener
}

func NewCoreSettingsStore(database Database, opener SecretValueOpener) *CoreSettingsStore {
	return &CoreSettingsStore{database: database, opener: opener}
}

func (store *CoreSettingsStore) Load(ctx context.Context) (singbox.Settings, error) {
	if store == nil || store.database == nil || store.opener == nil {
		return singbox.Settings{}, errors.New("core settings dependencies are required")
	}
	var settings singbox.Settings
	var configured bool
	var vlessPort, hysteria2Port, tuicPort, anyTLSPort, realityServerPort int
	var realityNonce, realityCiphertext []byte
	err := store.database.QueryRow(ctx, `
		SELECT configured,
		       COALESCE(host(listen_ipv4), ''),
		       COALESCE(host(listen_ipv6), ''),
		       vless_port,
		       hysteria2_port,
		       tuic_port,
		       anytls_port,
		       tls_server_name,
		       tls_certificate_path,
		       tls_key_path,
		       reality_server,
		       reality_server_port,
		       reality_private_key_nonce,
		       reality_private_key_ciphertext,
		       reality_short_id,
		       stats_listen,
		       allow_ipv4_outbound
		FROM core_settings
		WHERE singleton = TRUE`).Scan(
		&configured,
		&settings.ListenIPv4,
		&settings.ListenIPv6,
		&vlessPort,
		&hysteria2Port,
		&tuicPort,
		&anyTLSPort,
		&settings.TLSServerName,
		&settings.TLSCertificatePath,
		&settings.TLSKeyPath,
		&settings.RealityServer,
		&realityServerPort,
		&realityNonce,
		&realityCiphertext,
		&settings.RealityShortID,
		&settings.StatsListen,
		&settings.AllowIPv4Outbound,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return singbox.Settings{}, ErrCoreNotConfigured
	}
	if err != nil {
		return singbox.Settings{}, fmt.Errorf("load core settings: %w", err)
	}
	if !configured {
		return singbox.Settings{}, ErrCoreNotConfigured
	}
	if !validPort(vlessPort) || !validPort(hysteria2Port) || !validPort(tuicPort) || !validPort(anyTLSPort) || !validPort(realityServerPort) {
		return singbox.Settings{}, errors.New("persisted core port is invalid")
	}
	privateKey, err := store.opener.Open(realityPrivateKeyPurpose, secrets.SealedValue{
		Nonce: realityNonce, Ciphertext: realityCiphertext,
	})
	if err != nil {
		return singbox.Settings{}, fmt.Errorf("decrypt REALITY private key: %w", err)
	}
	settings.VLESSPort = uint16(vlessPort)
	settings.Hysteria2Port = uint16(hysteria2Port)
	settings.TUICPort = uint16(tuicPort)
	settings.AnyTLSPort = uint16(anyTLSPort)
	settings.RealityServerPort = uint16(realityServerPort)
	settings.RealityPrivateKey = privateKey
	return settings, nil
}

func validPort(port int) bool {
	return port > 0 && port <= 65535
}
