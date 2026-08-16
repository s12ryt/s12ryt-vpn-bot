package domain

type CoreSettingsOverview struct {
	Configured           bool   `json:"configured"`
	ListenIPv4           string `json:"listen_ipv4"`
	ListenIPv6           string `json:"listen_ipv6"`
	VLESSPort            uint16 `json:"vless_port"`
	Hysteria2Port        uint16 `json:"hysteria2_port"`
	TUICPort             uint16 `json:"tuic_port"`
	AnyTLSPort           uint16 `json:"anytls_port"`
	TLSServerName        string `json:"tls_server_name"`
	TLSCertificatePath   string `json:"tls_certificate_path"`
	TLSKeyPath           string `json:"tls_key_path"`
	RealityServer        string `json:"reality_server"`
	RealityServerPort    uint16 `json:"reality_server_port"`
	RealityShortID       string `json:"reality_short_id"`
	StatsListen          string `json:"stats_listen"`
	AllowIPv4Outbound    bool   `json:"allow_ipv4_outbound"`
	HasRealityPrivateKey bool   `json:"has_reality_private_key"`
}

type CoreSettingsUpdate struct {
	CoreSettingsOverview
	RealityPrivateKey string `json:"reality_private_key"`
}
