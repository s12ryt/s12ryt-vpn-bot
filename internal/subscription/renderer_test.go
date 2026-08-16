package subscription

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/singbox"
)

func TestRenderBase64IncludesFourProtocolsForBothAddressFamilies(t *testing.T) {
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	settings := singbox.Settings{
		ListenIPv4: "198.51.100.10", ListenIPv6: "2001:db8::10",
		VLESSPort: 443, Hysteria2Port: 443, TUICPort: 8443, AnyTLSPort: 8443,
		TLSServerName: "vpn.example.com", RealityServer: "www.example.com",
		RealityPrivateKey: base64.RawURLEncoding.EncodeToString(privateKey.Bytes()), RealityShortID: "a1b2",
	}
	bundle := domain.CredentialBundle{
		SubscriptionToken: strings.Repeat("A", 43),
		VLESSUUID:         "123e4567-e89b-42d3-a456-426614174000",
		Hysteria2Password: "hy-secret", TUICUUID: "223e4567-e89b-42d3-a456-426614174000",
		TUICPassword: "tuic-secret", AnyTLSPassword: "any-secret",
	}

	encoded, err := (Renderer{}).RenderBase64(settings, bundle)
	if err != nil {
		t.Fatalf("RenderBase64() error = %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode subscription: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(decoded)), "\n")
	if len(lines) != 8 {
		t.Fatalf("node count = %d, want 8: %q", len(lines), lines)
	}
	for _, prefix := range []string{"vless://", "hysteria2://", "tuic://", "anytls://"} {
		count := 0
		for _, line := range lines {
			if strings.HasPrefix(line, prefix) {
				count++
			}
		}
		if count != 2 {
			t.Fatalf("%s node count = %d, want 2", prefix, count)
		}
	}
	output := string(decoded)
	if strings.Contains(output, bundle.SubscriptionToken) || strings.Contains(output, settings.RealityPrivateKey) {
		t.Fatal("subscription leaked token or REALITY private key")
	}
	publicKey := base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes())
	if !strings.Contains(output, "pbk="+publicKey) || !strings.Contains(output, "[2001:db8::10]") {
		t.Fatalf("subscription missing REALITY public key or bracketed IPv6: %s", output)
	}
}

func TestRenderBase64RejectsInvalidRealityPrivateKey(t *testing.T) {
	_, err := (Renderer{}).RenderBase64(singbox.Settings{ListenIPv4: "198.51.100.10", RealityPrivateKey: "invalid"}, domain.CredentialBundle{})
	if err == nil {
		t.Fatal("RenderBase64() error = nil, want error")
	}
}
