package subscription

import (
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/singbox"
)

type Renderer struct{}

func (Renderer) RenderBase64(settings singbox.Settings, bundle domain.CredentialBundle) (string, error) {
	publicKey, err := realityPublicKey(settings.RealityPrivateKey)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(settings.TLSServerName) == "" || strings.TrimSpace(settings.RealityServer) == "" ||
		settings.VLESSPort == 0 || settings.Hysteria2Port == 0 || settings.TUICPort == 0 || settings.AnyTLSPort == 0 ||
		strings.TrimSpace(bundle.VLESSUUID) == "" || strings.TrimSpace(bundle.Hysteria2Password) == "" ||
		strings.TrimSpace(bundle.TUICUUID) == "" || strings.TrimSpace(bundle.TUICPassword) == "" || strings.TrimSpace(bundle.AnyTLSPassword) == "" {
		return "", errors.New("subscription settings and credentials are incomplete")
	}
	addresses := make([]string, 0, 2)
	if settings.ListenIPv4 != "" {
		addresses = append(addresses, settings.ListenIPv4)
	}
	if settings.ListenIPv6 != "" {
		addresses = append(addresses, settings.ListenIPv6)
	}
	if len(addresses) == 0 {
		return "", errors.New("subscription has no public address")
	}

	lines := make([]string, 0, len(addresses)*4)
	for _, address := range addresses {
		family := "IPv4"
		if strings.Contains(address, ":") {
			family = "IPv6"
		}
		lines = append(lines,
			buildURI("vless", url.User(bundle.VLESSUUID), address, settings.VLESSPort, url.Values{
				"encryption": {"none"}, "flow": {"xtls-rprx-vision"}, "security": {"reality"},
				"sni": {settings.RealityServer}, "fp": {"chrome"}, "pbk": {publicKey}, "sid": {settings.RealityShortID}, "type": {"tcp"},
			}, "VLESS "+family),
			buildURI("hysteria2", url.User(bundle.Hysteria2Password), address, settings.Hysteria2Port, url.Values{"sni": {settings.TLSServerName}}, "Hysteria2 "+family),
			buildURI("tuic", url.UserPassword(bundle.TUICUUID, bundle.TUICPassword), address, settings.TUICPort, url.Values{
				"sni": {settings.TLSServerName}, "congestion_control": {"bbr"}, "udp_relay_mode": {"native"}, "alpn": {"h3"},
			}, "TUIC "+family),
			buildURI("anytls", url.User(bundle.AnyTLSPassword), address, settings.AnyTLSPort, url.Values{"security": {"tls"}, "sni": {settings.TLSServerName}}, "AnyTLS "+family),
		)
	}
	plain := strings.Join(lines, "\n") + "\n"
	return base64.StdEncoding.EncodeToString([]byte(plain)), nil
}

func realityPublicKey(encodedPrivateKey string) (string, error) {
	privateBytes, err := base64.RawURLEncoding.DecodeString(encodedPrivateKey)
	if err != nil || len(privateBytes) != 32 || base64.RawURLEncoding.EncodeToString(privateBytes) != encodedPrivateKey {
		return "", errors.New("REALITY private key is invalid")
	}
	privateKey, err := ecdh.X25519().NewPrivateKey(privateBytes)
	if err != nil {
		return "", errors.New("REALITY private key is invalid")
	}
	return base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes()), nil
}

func buildURI(scheme string, user *url.Userinfo, address string, port uint16, query url.Values, name string) string {
	return (&url.URL{
		Scheme: scheme, User: user, Host: net.JoinHostPort(address, strconv.FormatUint(uint64(port), 10)),
		RawQuery: query.Encode(), Fragment: name,
	}).String()
}
