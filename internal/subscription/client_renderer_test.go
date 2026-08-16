package subscription

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/singbox"
)

func subscriptionFixture(t *testing.T) (singbox.Settings, domain.CredentialBundle) {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return singbox.Settings{ListenIPv4: "198.51.100.10", ListenIPv6: "2001:db8::10", VLESSPort: 443, Hysteria2Port: 443, TUICPort: 8443, AnyTLSPort: 8443, TLSServerName: "vpn.example.com", RealityServer: "www.example.com", RealityPrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()), RealityShortID: "a1b2"}, domain.CredentialBundle{SubscriptionToken: strings.Repeat("A", 43), VLESSUUID: "123e4567-e89b-42d3-a456-426614174000", Hysteria2Password: "hy-secret", TUICUUID: "223e4567-e89b-42d3-a456-426614174000", TUICPassword: "tuic-secret", AnyTLSPassword: "any-secret"}
}

func TestRenderSingBoxProducesEightSafeOutbounds(t *testing.T) {
	settings, bundle := subscriptionFixture(t)
	data, err := (Renderer{}).RenderSingBox(settings, bundle)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Outbounds) != 8 {
		t.Fatalf("outbounds = %d, want 8", len(document.Outbounds))
	}
	if strings.Contains(string(data), bundle.SubscriptionToken) || strings.Contains(string(data), settings.RealityPrivateKey) {
		t.Fatal("sing-box output leaked private data")
	}
}

func TestRenderClashProducesEightSupportedProxies(t *testing.T) {
	settings, bundle := subscriptionFixture(t)
	data, err := (Renderer{}).RenderClash(settings, bundle)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Proxies []map[string]any `json:"proxies"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("valid YAML 1.2 JSON document: %v", err)
	}
	if len(document.Proxies) != 8 {
		t.Fatalf("proxies = %d, want 8", len(document.Proxies))
	}
	types := map[string]bool{}
	for _, proxy := range document.Proxies {
		types[proxy["type"].(string)] = true
	}
	for _, protocol := range []string{"vless", "hysteria2", "tuic", "anytls"} {
		if !types[protocol] {
			t.Fatalf("missing %s proxy", protocol)
		}
	}
	if strings.Contains(string(data), bundle.SubscriptionToken) || strings.Contains(string(data), settings.RealityPrivateKey) {
		t.Fatal("Mihomo output leaked private data")
	}
}
