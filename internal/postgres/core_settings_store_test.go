package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/secrets"
)

func TestCoreSettingsStoreLoadsConfiguredSettingsAndDecryptsRealityKey(t *testing.T) {
	opener := &secretValueOpenerStub{value: "reality-private-key"}
	database := &coreSettingsDatabaseStub{row: &coreSettingsRowStub{
		configured:        true,
		listenIPv4:        "203.0.113.10",
		listenIPv6:        "2001:db8::10",
		vlessPort:         443,
		hysteria2Port:     443,
		tuicPort:          8443,
		anyTLSPort:        8443,
		tlsServerName:     "vpn.example.com",
		tlsCertificate:    "/run/tls/fullchain.pem",
		tlsKey:            "/run/tls/privkey.pem",
		realityServer:     "www.example.com",
		realityServerPort: 443,
		realityNonce:      []byte("123456789012"),
		realityCiphertext: []byte("authenticated-private-key"),
		realityShortID:    "0123456789abcdef",
		statsListen:       "127.0.0.1:10085",
		allowIPv4:         false,
	}}
	store := NewCoreSettingsStore(database, opener)

	settings, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if settings.ListenIPv4 != "203.0.113.10" || settings.ListenIPv6 != "2001:db8::10" ||
		settings.VLESSPort != 443 || settings.Hysteria2Port != 443 || settings.TUICPort != 8443 || settings.AnyTLSPort != 8443 ||
		settings.TLSServerName != "vpn.example.com" || settings.TLSCertificatePath != "/run/tls/fullchain.pem" ||
		settings.TLSKeyPath != "/run/tls/privkey.pem" || settings.RealityServer != "www.example.com" ||
		settings.RealityServerPort != 443 || settings.RealityPrivateKey != "reality-private-key" ||
		settings.RealityShortID != "0123456789abcdef" || settings.StatsListen != "127.0.0.1:10085" || settings.AllowIPv4Outbound {
		t.Fatalf("Load() = %#v", settings)
	}
	if opener.purpose != "sing-box/reality-private-key" || string(opener.sealed.Nonce) != "123456789012" ||
		string(opener.sealed.Ciphertext) != "authenticated-private-key" {
		t.Fatalf("Open() purpose=%q sealed=%#v", opener.purpose, opener.sealed)
	}
	if !strings.Contains(database.query, "core_settings") || !strings.Contains(database.query, "host(listen_ipv4)") {
		t.Fatalf("settings query = %q", database.query)
	}
}

func TestCoreSettingsStoreRejectsUnconfiguredOrUndecryptableSettings(t *testing.T) {
	tests := []struct {
		name    string
		row     *coreSettingsRowStub
		opener  *secretValueOpenerStub
		wantErr error
	}{
		{name: "missing row", row: &coreSettingsRowStub{err: pgx.ErrNoRows}, opener: &secretValueOpenerStub{}, wantErr: ErrCoreNotConfigured},
		{name: "not configured", row: &coreSettingsRowStub{configured: false}, opener: &secretValueOpenerStub{}, wantErr: ErrCoreNotConfigured},
		{
			name: "secret cannot decrypt",
			row: &coreSettingsRowStub{
				configured: true, listenIPv4: "203.0.113.10", vlessPort: 443, hysteria2Port: 443, tuicPort: 8443, anyTLSPort: 8443,
				tlsServerName: "vpn.example.com", tlsCertificate: "/cert", tlsKey: "/key", realityServer: "example.com",
				realityServerPort: 443, realityNonce: []byte("123456789012"), realityCiphertext: []byte("ciphertext payload"),
				realityShortID: "abcd", statsListen: "127.0.0.1:10085",
			},
			opener:  &secretValueOpenerStub{err: errors.New("decrypt failed")},
			wantErr: errors.New("decrypt failed"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := NewCoreSettingsStore(&coreSettingsDatabaseStub{row: test.row}, test.opener)
			settings, err := store.Load(context.Background())
			if !errors.Is(err, test.wantErr) && (test.name != "secret cannot decrypt" || err == nil || !strings.Contains(err.Error(), "decrypt")) {
				t.Fatalf("Load() error = %v", err)
			}
			if settings.RealityPrivateKey != "" {
				t.Fatalf("Load() returned secret on error: %#v", settings)
			}
		})
	}
}

type secretValueOpenerStub struct {
	purpose string
	sealed  secrets.SealedValue
	value   string
	err     error
}

func (stub *secretValueOpenerStub) Open(purpose string, sealed secrets.SealedValue) (string, error) {
	stub.purpose = purpose
	stub.sealed = sealed
	return stub.value, stub.err
}

type coreSettingsDatabaseStub struct {
	query string
	row   pgx.Row
}

func (stub *coreSettingsDatabaseStub) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	stub.query = query
	return stub.row
}

func (stub *coreSettingsDatabaseStub) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}

type coreSettingsRowStub struct {
	configured        bool
	listenIPv4        string
	listenIPv6        string
	vlessPort         int
	hysteria2Port     int
	tuicPort          int
	anyTLSPort        int
	tlsServerName     string
	tlsCertificate    string
	tlsKey            string
	realityServer     string
	realityServerPort int
	realityNonce      []byte
	realityCiphertext []byte
	realityShortID    string
	statsListen       string
	allowIPv4         bool
	err               error
}

func (row *coreSettingsRowStub) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != 17 {
		return errors.New("unexpected core settings destination count")
	}
	*destinations[0].(*bool) = row.configured
	*destinations[1].(*string) = row.listenIPv4
	*destinations[2].(*string) = row.listenIPv6
	*destinations[3].(*int) = row.vlessPort
	*destinations[4].(*int) = row.hysteria2Port
	*destinations[5].(*int) = row.tuicPort
	*destinations[6].(*int) = row.anyTLSPort
	*destinations[7].(*string) = row.tlsServerName
	*destinations[8].(*string) = row.tlsCertificate
	*destinations[9].(*string) = row.tlsKey
	*destinations[10].(*string) = row.realityServer
	*destinations[11].(*int) = row.realityServerPort
	*destinations[12].(*[]byte) = row.realityNonce
	*destinations[13].(*[]byte) = row.realityCiphertext
	*destinations[14].(*string) = row.realityShortID
	*destinations[15].(*string) = row.statsListen
	*destinations[16].(*bool) = row.allowIPv4
	return nil
}
