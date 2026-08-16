package subscription

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/singbox"
)

func (Renderer) RenderSingBox(settings singbox.Settings, bundle domain.CredentialBundle) ([]byte, error) {
	publicKey, addresses, err := subscriptionInputs(settings, bundle)
	if err != nil {
		return nil, err
	}
	outbounds := make([]map[string]any, 0, len(addresses)*4)
	for _, address := range addresses {
		family := addressFamily(address)
		outbounds = append(outbounds,
			map[string]any{"type": "vless", "tag": "VLESS " + family, "server": address, "server_port": settings.VLESSPort, "uuid": bundle.VLESSUUID, "flow": "xtls-rprx-vision", "tls": map[string]any{"enabled": true, "server_name": settings.RealityServer, "utls": map[string]any{"enabled": true, "fingerprint": "chrome"}, "reality": map[string]any{"enabled": true, "public_key": publicKey, "short_id": settings.RealityShortID}}},
			map[string]any{"type": "hysteria2", "tag": "Hysteria2 " + family, "server": address, "server_port": settings.Hysteria2Port, "password": bundle.Hysteria2Password, "tls": map[string]any{"enabled": true, "server_name": settings.TLSServerName}},
			map[string]any{"type": "tuic", "tag": "TUIC " + family, "server": address, "server_port": settings.TUICPort, "uuid": bundle.TUICUUID, "password": bundle.TUICPassword, "congestion_control": "bbr", "udp_relay_mode": "native", "tls": map[string]any{"enabled": true, "server_name": settings.TLSServerName, "alpn": []string{"h3"}}},
			map[string]any{"type": "anytls", "tag": "AnyTLS " + family, "server": address, "server_port": settings.AnyTLSPort, "password": bundle.AnyTLSPassword, "tls": map[string]any{"enabled": true, "server_name": settings.TLSServerName}},
		)
	}
	return json.MarshalIndent(map[string]any{"log": map[string]any{"level": "warn"}, "outbounds": outbounds, "route": map[string]any{"final": outbounds[0]["tag"]}}, "", "  ")
}

func (Renderer) RenderClash(settings singbox.Settings, bundle domain.CredentialBundle) ([]byte, error) {
	publicKey, addresses, err := subscriptionInputs(settings, bundle)
	if err != nil {
		return nil, err
	}
	proxies := make([]map[string]any, 0, len(addresses)*4)
	names := make([]string, 0, len(addresses)*4)
	for _, address := range addresses {
		family := addressFamily(address)
		entries := []map[string]any{
			{"name": "VLESS " + family, "type": "vless", "server": address, "port": settings.VLESSPort, "uuid": bundle.VLESSUUID, "network": "tcp", "tls": true, "udp": true, "flow": "xtls-rprx-vision", "servername": settings.RealityServer, "client-fingerprint": "chrome", "reality-opts": map[string]any{"public-key": publicKey, "short-id": settings.RealityShortID}},
			{"name": "Hysteria2 " + family, "type": "hysteria2", "server": address, "port": settings.Hysteria2Port, "password": bundle.Hysteria2Password, "sni": settings.TLSServerName, "skip-cert-verify": false},
			{"name": "TUIC " + family, "type": "tuic", "server": address, "port": settings.TUICPort, "uuid": bundle.TUICUUID, "password": bundle.TUICPassword, "sni": settings.TLSServerName, "alpn": []string{"h3"}, "udp-relay-mode": "native", "congestion-controller": "bbr"},
			{"name": "AnyTLS " + family, "type": "anytls", "server": address, "port": settings.AnyTLSPort, "password": bundle.AnyTLSPassword, "sni": settings.TLSServerName, "client-fingerprint": "chrome", "udp": true, "skip-cert-verify": false},
		}
		for _, entry := range entries {
			proxies = append(proxies, entry)
			names = append(names, entry["name"].(string))
		}
	}
	return json.MarshalIndent(map[string]any{"proxies": proxies, "proxy-groups": []any{map[string]any{"name": "VPN", "type": "select", "proxies": names}}, "rules": []string{"MATCH,VPN"}}, "", "  ")
}

func subscriptionInputs(settings singbox.Settings, bundle domain.CredentialBundle) (string, []string, error) {
	publicKey, err := realityPublicKey(settings.RealityPrivateKey)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(settings.TLSServerName) == "" || strings.TrimSpace(settings.RealityServer) == "" || settings.VLESSPort == 0 || settings.Hysteria2Port == 0 || settings.TUICPort == 0 || settings.AnyTLSPort == 0 || strings.TrimSpace(bundle.VLESSUUID) == "" || strings.TrimSpace(bundle.Hysteria2Password) == "" || strings.TrimSpace(bundle.TUICUUID) == "" || strings.TrimSpace(bundle.TUICPassword) == "" || strings.TrimSpace(bundle.AnyTLSPassword) == "" {
		return "", nil, errors.New("subscription settings and credentials are incomplete")
	}
	addresses := []string{}
	if settings.ListenIPv4 != "" {
		addresses = append(addresses, settings.ListenIPv4)
	}
	if settings.ListenIPv6 != "" {
		addresses = append(addresses, settings.ListenIPv6)
	}
	if len(addresses) == 0 {
		return "", nil, errors.New("subscription has no public address")
	}
	return publicKey, addresses, nil
}

func addressFamily(address string) string {
	if strings.Contains(address, ":") {
		return "IPv6"
	}
	return "IPv4"
}
