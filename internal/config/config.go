package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"
)

const (
	defaultWebIP             = "0.0.0.0"
	defaultPort              = 35699
	defaultCoreControlSocket = "/run/s12ryt/core-control.sock"
	defaultSingBoxConfigPath = "/var/lib/s12ryt/sing-box/config.json"
	defaultTrafficSpoolPath  = "/var/lib/s12ryt/traffic/pending.json"
	defaultWebAssetDir       = "/srv/s12ryt/web"
)

type Config struct {
	WebIP             string
	Port              uint16
	MasterKey         []byte
	BootstrapBotToken string
	DatabaseURL       string
	OwnerTelegramID   int64
	WebPublicURL      string
	TrustedProxyCIDRs []netip.Prefix
	CoreControlSocket string
	SingBoxConfigPath string
	TrafficSpoolPath  string
	WebAssetDir       string
}

func Load(lookup func(string) string) (Config, error) {
	masterKey, err := decodeMasterKey(lookup("APP_MASTER_KEY"))
	if err != nil {
		return Config{}, err
	}

	botToken := strings.TrimSpace(lookup("BOT_TOKEN"))
	if botToken == "" {
		return Config{}, errors.New("BOT_TOKEN is required for bootstrap")
	}
	databaseURL := strings.TrimSpace(lookup("DATABASE_URL"))
	if databaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}

	ownerTelegramID, err := strconv.ParseInt(strings.TrimSpace(lookup("OWNER_TG_ID")), 10, 64)
	if err != nil || ownerTelegramID <= 0 {
		return Config{}, errors.New("OWNER_TG_ID must be a positive integer")
	}

	webIP := strings.TrimSpace(lookup("WEB_IP"))
	if webIP == "" {
		webIP = defaultWebIP
	}
	if net.ParseIP(webIP) == nil {
		return Config{}, errors.New("WEB_IP must be a valid IP address")
	}

	port, err := parsePort(lookup("PORT"))
	if err != nil {
		return Config{}, err
	}
	trustedProxyCIDRs, err := parseTrustedProxyCIDRs(lookup("TRUSTED_PROXY_CIDRS"))
	if err != nil {
		return Config{}, err
	}
	webPublicURL, err := parseWebPublicURL(lookup("WEB_PUBLIC_URL"))
	if err != nil {
		return Config{}, err
	}
	coreControlSocket, err := parseUnixFilePath("CORE_CONTROL_SOCKET", lookup("CORE_CONTROL_SOCKET"), defaultCoreControlSocket)
	if err != nil {
		return Config{}, err
	}
	singBoxConfigPath, err := parseUnixFilePath("SINGBOX_CONFIG_PATH", lookup("SINGBOX_CONFIG_PATH"), defaultSingBoxConfigPath)
	if err != nil {
		return Config{}, err
	}
	trafficSpoolPath, err := parseUnixFilePath("TRAFFIC_SPOOL_PATH", lookup("TRAFFIC_SPOOL_PATH"), defaultTrafficSpoolPath)
	if err != nil {
		return Config{}, err
	}
	webAssetDir, err := parseUnixFilePath("WEB_ASSET_DIR", lookup("WEB_ASSET_DIR"), defaultWebAssetDir)
	if err != nil {
		return Config{}, err
	}

	return Config{
		WebIP:             webIP,
		Port:              port,
		MasterKey:         masterKey,
		BootstrapBotToken: botToken,
		DatabaseURL:       databaseURL,
		OwnerTelegramID:   ownerTelegramID,
		WebPublicURL:      webPublicURL,
		TrustedProxyCIDRs: trustedProxyCIDRs,
		CoreControlSocket: coreControlSocket,
		SingBoxConfigPath: singBoxConfigPath,
		TrafficSpoolPath:  trafficSpoolPath,
		WebAssetDir:       webAssetDir,
	}, nil
}

func parseUnixFilePath(name, value, fallback string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	if !path.IsAbs(value) || path.Clean(value) != value || path.Base(value) == "." || path.Base(value) == "/" {
		return "", fmt.Errorf("%s must be a clean absolute Unix file path", name)
	}
	return value, nil
}

func parseWebPublicURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("WEB_PUBLIC_URL is required")
	}
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.Hostname() == "" {
		return "", errors.New("WEB_PUBLIC_URL must be an absolute HTTPS URL")
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return "", errors.New("WEB_PUBLIC_URL must use HTTPS")
	}
	if parsed.User != nil {
		return "", errors.New("WEB_PUBLIC_URL must not contain userinfo")
	}
	if parsed.RawQuery != "" {
		return "", errors.New("WEB_PUBLIC_URL must not contain a query")
	}
	if parsed.Fragment != "" {
		return "", errors.New("WEB_PUBLIC_URL must not contain a fragment")
	}
	parsed.Scheme = "https"
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return parsed.String(), nil
}

func parseTrustedProxyCIDRs(value string) ([]netip.Prefix, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	prefixes := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXY_CIDRS must contain valid comma-separated CIDRs: %w", err)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func (configuration Config) ListenAddress() string {
	return net.JoinHostPort(configuration.WebIP, strconv.FormatUint(uint64(configuration.Port), 10))
}

func decodeMasterKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("APP_MASTER_KEY is required")
	}
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("APP_MASTER_KEY must be valid Base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("APP_MASTER_KEY must decode to exactly 32 bytes, got %d", len(key))
	}
	return key, nil
}

func parsePort(value string) (uint16, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultPort, nil
	}
	port, err := strconv.ParseUint(value, 10, 16)
	if err != nil || port == 0 {
		return 0, errors.New("PORT must be an integer between 1 and 65535")
	}
	return uint16(port), nil
}
