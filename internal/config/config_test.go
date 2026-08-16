package config

import (
	"encoding/base64"
	"net/netip"
	"strings"
	"testing"
)

func TestLoadUsesConfirmedHTTPDefaults(t *testing.T) {
	env := validEnvironment()

	configuration, err := Load(mapLookup(env))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if configuration.WebIP != "0.0.0.0" {
		t.Fatalf("WebIP = %q, want 0.0.0.0", configuration.WebIP)
	}
	if configuration.Port != 35699 {
		t.Fatalf("Port = %d, want 35699", configuration.Port)
	}
	if configuration.OwnerTelegramID != 123456789 {
		t.Fatalf("OwnerTelegramID = %d, want 123456789", configuration.OwnerTelegramID)
	}
	if len(configuration.MasterKey) != 32 {
		t.Fatalf("MasterKey length = %d, want 32", len(configuration.MasterKey))
	}
	if configuration.WebPublicURL != "https://vpn.example.com/admin" {
		t.Fatalf("WebPublicURL = %q, want normalized HTTPS base URL", configuration.WebPublicURL)
	}
	if configuration.CoreControlSocket != "/run/s12ryt/core-control.sock" {
		t.Fatalf("CoreControlSocket = %q", configuration.CoreControlSocket)
	}
	if configuration.SingBoxConfigPath != "/var/lib/s12ryt/sing-box/config.json" {
		t.Fatalf("SingBoxConfigPath = %q", configuration.SingBoxConfigPath)
	}
	if configuration.TrafficSpoolPath != "/var/lib/s12ryt/traffic/pending.json" {
		t.Fatalf("TrafficSpoolPath = %q", configuration.TrafficSpoolPath)
	}
	if configuration.WebAssetDir != "/srv/s12ryt/web" {
		t.Fatalf("WebAssetDir = %q", configuration.WebAssetDir)
	}
}

func TestLoadRejectsInvalidBootstrapSecurityConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]string)
		wantErr string
	}{
		{
			name: "missing master key",
			mutate: func(env map[string]string) {
				delete(env, "APP_MASTER_KEY")
			},
			wantErr: "APP_MASTER_KEY",
		},
		{
			name: "short master key",
			mutate: func(env map[string]string) {
				env["APP_MASTER_KEY"] = base64.StdEncoding.EncodeToString(make([]byte, 16))
			},
			wantErr: "32 bytes",
		},
		{
			name: "invalid owner",
			mutate: func(env map[string]string) {
				env["OWNER_TG_ID"] = "0"
			},
			wantErr: "OWNER_TG_ID",
		},
		{
			name: "missing bot token",
			mutate: func(env map[string]string) {
				delete(env, "BOT_TOKEN")
			},
			wantErr: "BOT_TOKEN",
		},
		{
			name: "missing database URL",
			mutate: func(env map[string]string) {
				delete(env, "DATABASE_URL")
			},
			wantErr: "DATABASE_URL",
		},
		{
			name: "missing public URL",
			mutate: func(env map[string]string) {
				delete(env, "WEB_PUBLIC_URL")
			},
			wantErr: "WEB_PUBLIC_URL",
		},
		{
			name: "insecure public URL",
			mutate: func(env map[string]string) {
				env["WEB_PUBLIC_URL"] = "http://vpn.example.com"
			},
			wantErr: "HTTPS",
		},
		{
			name: "public URL with query",
			mutate: func(env map[string]string) {
				env["WEB_PUBLIC_URL"] = "https://vpn.example.com?tenant=one"
			},
			wantErr: "query",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := validEnvironment()
			test.mutate(env)

			_, err := Load(mapLookup(env))
			if err == nil {
				t.Fatal("Load() error = nil, want bootstrap validation error")
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Load() error = %q, want it to contain %q", err, test.wantErr)
			}
		})
	}
}

func TestLoadValidatesHTTPOverrides(t *testing.T) {
	env := validEnvironment()
	env["WEB_IP"] = "127.0.0.1"
	env["PORT"] = "8443"

	configuration, err := Load(mapLookup(env))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if configuration.WebIP != "127.0.0.1" || configuration.Port != 8443 {
		t.Fatalf("HTTP address = %s:%d, want 127.0.0.1:8443", configuration.WebIP, configuration.Port)
	}

	env["PORT"] = "70000"
	if _, err := Load(mapLookup(env)); err == nil {
		t.Fatal("Load() accepted a port outside the TCP range")
	}
}

func TestLoadValidatesCoreRuntimeUnixPaths(t *testing.T) {
	env := validEnvironment()
	env["CORE_CONTROL_SOCKET"] = "/custom/run/control.sock"
	env["SINGBOX_CONFIG_PATH"] = "/custom/state/config.json"
	env["TRAFFIC_SPOOL_PATH"] = "/custom/state/traffic.json"
	env["WEB_ASSET_DIR"] = "/custom/web"

	configuration, err := Load(mapLookup(env))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if configuration.CoreControlSocket != "/custom/run/control.sock" ||
		configuration.SingBoxConfigPath != "/custom/state/config.json" ||
		configuration.TrafficSpoolPath != "/custom/state/traffic.json" || configuration.WebAssetDir != "/custom/web" {
		t.Fatalf("runtime paths = %q, %q, %q, %q", configuration.CoreControlSocket, configuration.SingBoxConfigPath, configuration.TrafficSpoolPath, configuration.WebAssetDir)
	}

	for _, test := range []struct {
		key   string
		value string
	}{
		{key: "CORE_CONTROL_SOCKET", value: "relative/control.sock"},
		{key: "CORE_CONTROL_SOCKET", value: "/run/../control.sock"},
		{key: "SINGBOX_CONFIG_PATH", value: "relative/config.json"},
		{key: "SINGBOX_CONFIG_PATH", value: "/var/lib/../config.json"},
		{key: "TRAFFIC_SPOOL_PATH", value: "relative/pending.json"},
		{key: "TRAFFIC_SPOOL_PATH", value: "/var/lib/../pending.json"},
		{key: "WEB_ASSET_DIR", value: "relative/web"},
		{key: "WEB_ASSET_DIR", value: "/srv/../web"},
	} {
		t.Run(test.key+"="+test.value, func(t *testing.T) {
			invalid := validEnvironment()
			invalid[test.key] = test.value
			if _, err := Load(mapLookup(invalid)); err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("Load() error = %v, want %s path validation", err, test.key)
			}
		})
	}
}

func TestListenAddressSupportsIPv6(t *testing.T) {
	configuration := Config{WebIP: "::1", Port: 35699}

	if got := configuration.ListenAddress(); got != "[::1]:35699" {
		t.Fatalf("ListenAddress() = %q, want [::1]:35699", got)
	}
}

func TestLoadParsesTrustedProxyCIDRsAndDefaultsToTrustingNone(t *testing.T) {
	env := validEnvironment()
	configuration, err := Load(mapLookup(env))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(configuration.TrustedProxyCIDRs) != 0 {
		t.Fatalf("default TrustedProxyCIDRs = %#v, want empty", configuration.TrustedProxyCIDRs)
	}

	env["TRUSTED_PROXY_CIDRS"] = "10.0.0.0/8, 2001:db8::/32"
	configuration, err = Load(mapLookup(env))
	if err != nil {
		t.Fatalf("Load(with proxies) error = %v", err)
	}
	want := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("2001:db8::/32")}
	if len(configuration.TrustedProxyCIDRs) != len(want) {
		t.Fatalf("TrustedProxyCIDRs = %#v, want %#v", configuration.TrustedProxyCIDRs, want)
	}
	for index := range want {
		if configuration.TrustedProxyCIDRs[index] != want[index] {
			t.Fatalf("TrustedProxyCIDRs[%d] = %v, want %v", index, configuration.TrustedProxyCIDRs[index], want[index])
		}
	}

	env["TRUSTED_PROXY_CIDRS"] = "10.0.0.0/8,invalid"
	if _, err := Load(mapLookup(env)); err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
		t.Fatalf("Load(invalid proxy) error = %v, want TRUSTED_PROXY_CIDRS validation", err)
	}
}

func validEnvironment() map[string]string {
	return map[string]string{
		"APP_MASTER_KEY": base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"BOT_TOKEN":      "123456:bootstrap-token",
		"DATABASE_URL":   "postgres://vpn:secret@database:5432/vpn?sslmode=disable",
		"OWNER_TG_ID":    "123456789",
		"WEB_PUBLIC_URL": "https://vpn.example.com/admin/",
	}
}

func mapLookup(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}
