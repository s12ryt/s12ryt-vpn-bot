package singbox

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

var uuidV4Pattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-4[0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

type User struct {
	TelegramID  int64
	Credentials domain.CredentialBundle
}

type Settings struct {
	ListenIPv4         string
	ListenIPv6         string
	VLESSPort          uint16
	Hysteria2Port      uint16
	TUICPort           uint16
	AnyTLSPort         uint16
	TLSServerName      string
	TLSCertificatePath string
	TLSKeyPath         string
	RealityServer      string
	RealityServerPort  uint16
	RealityPrivateKey  string
	RealityShortID     string
	StatsListen        string
	AllowIPv4Outbound  bool
	Users              []User
}

type Generator struct{}

func (Generator) Generate(settings Settings) ([]byte, error) {
	users, err := validateAndSort(settings)
	if err != nil {
		return nil, err
	}

	addresses := make([]listenAddress, 0, 2)
	if settings.ListenIPv4 != "" {
		addresses = append(addresses, listenAddress{family: "ipv4", address: settings.ListenIPv4})
	}
	if settings.ListenIPv6 != "" {
		addresses = append(addresses, listenAddress{family: "ipv6", address: settings.ListenIPv6})
	}

	inbounds := make([]any, 0, len(addresses)*4)
	inboundTags := make([]string, 0, len(addresses)*4)
	for _, protocol := range []string{"vless", "hysteria2", "tuic", "anytls"} {
		for _, address := range addresses {
			tag := protocol + "-" + address.family
			inboundTags = append(inboundTags, tag)
			inbounds = append(inbounds, buildInbound(protocol, tag, address.address, settings, users))
		}
	}

	userNames := make([]string, len(users))
	for index, user := range users {
		userNames[index] = strconv.FormatInt(user.TelegramID, 10)
	}
	resolverStrategy := "ipv6_only"
	routeRules := []any{map[string]any{"ip_version": 4, "action": "reject"}}
	if settings.AllowIPv4Outbound {
		resolverStrategy = "prefer_ipv6"
		routeRules = nil
	}
	routeRules = append(routeRules, map[string]any{"action": "route", "outbound": "direct"})

	document := map[string]any{
		"log": map[string]any{"level": "info", "timestamp": true},
		"dns": map[string]any{
			"servers":  []any{map[string]any{"type": "local", "tag": "local"}},
			"strategy": resolverStrategy,
		},
		"inbounds": inbounds,
		"outbounds": []any{map[string]any{
			"type": "direct", "tag": "direct",
			"domain_resolver": map[string]any{"server": "local", "strategy": resolverStrategy},
		}},
		"route": map[string]any{"rules": routeRules, "final": "direct"},
		"experimental": map[string]any{"v2ray_api": map[string]any{
			"listen": settings.StatsListen,
			"stats": map[string]any{
				"enabled": true, "inbounds": inboundTags, "users": userNames,
			},
		}},
	}
	generated, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode sing-box configuration: %w", err)
	}
	return generated, nil
}

type listenAddress struct {
	family  string
	address string
}

func validateAndSort(settings Settings) ([]User, error) {
	if err := validateListenAddress(settings.ListenIPv4, false); err != nil {
		return nil, err
	}
	if err := validateListenAddress(settings.ListenIPv6, true); err != nil {
		return nil, err
	}
	if settings.ListenIPv4 == "" && settings.ListenIPv6 == "" {
		return nil, errors.New("at least one inbound listen address is required")
	}
	if settings.VLESSPort == 0 || settings.Hysteria2Port == 0 || settings.TUICPort == 0 || settings.AnyTLSPort == 0 {
		return nil, errors.New("all protocol ports are required")
	}
	if settings.VLESSPort == settings.AnyTLSPort {
		return nil, errors.New("VLESS and AnyTLS TCP ports conflict")
	}
	if settings.Hysteria2Port == settings.TUICPort {
		return nil, errors.New("Hysteria2 and TUIC UDP ports conflict")
	}
	if strings.TrimSpace(settings.TLSServerName) == "" || strings.TrimSpace(settings.TLSCertificatePath) == "" || strings.TrimSpace(settings.TLSKeyPath) == "" {
		return nil, errors.New("TLS server name and certificate paths are required")
	}
	if strings.TrimSpace(settings.RealityServer) == "" || settings.RealityServerPort == 0 || strings.TrimSpace(settings.RealityPrivateKey) == "" {
		return nil, errors.New("REALITY handshake and private key settings are required")
	}
	privateKey, err := base64.RawURLEncoding.DecodeString(settings.RealityPrivateKey)
	if err != nil || len(privateKey) != 32 || base64.RawURLEncoding.EncodeToString(privateKey) != settings.RealityPrivateKey {
		return nil, errors.New("REALITY private key must be a canonical 32-byte Base64URL value")
	}
	if _, err := ecdh.X25519().NewPrivateKey(privateKey); err != nil {
		return nil, errors.New("REALITY private key is invalid")
	}
	if !validShortID(settings.RealityShortID) {
		return nil, errors.New("REALITY short ID must contain at most 16 hexadecimal characters")
	}
	if err := validateStatsListen(settings.StatsListen); err != nil {
		return nil, err
	}

	users := append([]User(nil), settings.Users...)
	sort.Slice(users, func(left, right int) bool { return users[left].TelegramID < users[right].TelegramID })
	for index, user := range users {
		if user.TelegramID <= 0 {
			return nil, errors.New("user Telegram ID must be positive")
		}
		if index > 0 && users[index-1].TelegramID == user.TelegramID {
			return nil, errors.New("duplicate user Telegram ID")
		}
		if err := validateCredentials(user.Credentials); err != nil {
			return nil, fmt.Errorf("validate credentials for Telegram ID %d: %w", user.TelegramID, err)
		}
	}
	return users, nil
}

func validateListenAddress(value string, wantIPv6 bool) error {
	if value == "" {
		return nil
	}
	address, err := netip.ParseAddr(value)
	if err != nil || address.Is4In6() || address.Is6() != wantIPv6 || address.Is4() == wantIPv6 {
		family := "IPv4"
		if wantIPv6 {
			family = "IPv6"
		}
		return fmt.Errorf("listen address must be a valid %s address", family)
	}
	return nil
}

func validateStatsListen(value string) error {
	host, port, err := net.SplitHostPort(value)
	if err != nil || port == "" {
		return errors.New("stats listen address must be a localhost host:port")
	}
	address, err := netip.ParseAddr(host)
	if err != nil || !address.IsLoopback() {
		return errors.New("stats listen address must use a loopback IP")
	}
	parsedPort, err := strconv.ParseUint(port, 10, 16)
	if err != nil || parsedPort == 0 {
		return errors.New("stats listen port is invalid")
	}
	return nil
}

func validShortID(value string) bool {
	if len(value) > 16 || len(value)%2 != 0 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return false
		}
	}
	return true
}

func validateCredentials(bundle domain.CredentialBundle) error {
	token, err := base64.RawURLEncoding.DecodeString(bundle.SubscriptionToken)
	if err != nil || len(token) != 32 || base64.RawURLEncoding.EncodeToString(token) != bundle.SubscriptionToken {
		return errors.New("subscription token is invalid")
	}
	if !uuidV4Pattern.MatchString(bundle.VLESSUUID) || !uuidV4Pattern.MatchString(bundle.TUICUUID) {
		return errors.New("protocol UUID is invalid")
	}
	if strings.TrimSpace(bundle.Hysteria2Password) == "" || strings.TrimSpace(bundle.TUICPassword) == "" || strings.TrimSpace(bundle.AnyTLSPassword) == "" {
		return errors.New("protocol password is required")
	}
	return nil
}

func buildInbound(protocol, tag, listen string, settings Settings, users []User) map[string]any {
	inbound := map[string]any{"type": protocol, "tag": tag, "listen": listen}
	switch protocol {
	case "vless":
		inbound["listen_port"] = settings.VLESSPort
		inbound["users"] = mapUsers(users, func(user User) map[string]any {
			return map[string]any{"name": strconv.FormatInt(user.TelegramID, 10), "uuid": user.Credentials.VLESSUUID, "flow": "xtls-rprx-vision"}
		})
		inbound["tls"] = map[string]any{
			"enabled": true,
			"reality": map[string]any{
				"enabled":     true,
				"handshake":   map[string]any{"server": settings.RealityServer, "server_port": settings.RealityServerPort},
				"private_key": settings.RealityPrivateKey,
				"short_id":    []string{settings.RealityShortID},
			},
		}
	case "hysteria2":
		inbound["listen_port"] = settings.Hysteria2Port
		inbound["users"] = mapUsers(users, func(user User) map[string]any {
			return map[string]any{"name": strconv.FormatInt(user.TelegramID, 10), "password": user.Credentials.Hysteria2Password}
		})
		inbound["tls"] = certificateTLS(settings)
	case "tuic":
		inbound["listen_port"] = settings.TUICPort
		inbound["users"] = mapUsers(users, func(user User) map[string]any {
			return map[string]any{
				"name": strconv.FormatInt(user.TelegramID, 10), "uuid": user.Credentials.TUICUUID, "password": user.Credentials.TUICPassword,
			}
		})
		inbound["zero_rtt_handshake"] = false
		inbound["tls"] = certificateTLS(settings)
	case "anytls":
		inbound["listen_port"] = settings.AnyTLSPort
		inbound["users"] = mapUsers(users, func(user User) map[string]any {
			return map[string]any{"name": strconv.FormatInt(user.TelegramID, 10), "password": user.Credentials.AnyTLSPassword}
		})
		inbound["tls"] = certificateTLS(settings)
	}
	return inbound
}

func certificateTLS(settings Settings) map[string]any {
	return map[string]any{
		"enabled": true, "server_name": settings.TLSServerName,
		"certificate_path": settings.TLSCertificatePath, "key_path": settings.TLSKeyPath,
		"min_version": "1.2",
	}
}

func mapUsers(users []User, convert func(User) map[string]any) []any {
	result := make([]any, len(users))
	for index, user := range users {
		result[index] = convert(user)
	}
	return result
}
