package singbox

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

func TestGeneratorBuildsDualStackFourProtocolConfigurationWithSharedUserNames(t *testing.T) {
	settings := testSettings()
	settings.Users = []User{
		{TelegramID: 2002, Credentials: testCredentialBundle("b")},
		{TelegramID: 1001, Credentials: testCredentialBundle("a")},
	}

	generated, err := (Generator{}).Generate(settings)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !json.Valid(generated) {
		t.Fatalf("Generate() returned invalid JSON: %s", generated)
	}
	if strings.Contains(string(generated), settings.Users[0].Credentials.SubscriptionToken) ||
		strings.Contains(string(generated), settings.Users[1].Credentials.SubscriptionToken) {
		t.Fatal("generated core configuration contains a subscription token")
	}

	var document map[string]any
	if err := json.Unmarshal(generated, &document); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	inbounds := objectSlice(t, document["inbounds"])
	if len(inbounds) != 8 {
		t.Fatalf("inbound count = %d, want 8", len(inbounds))
	}
	wantTags := []string{
		"vless-ipv4", "vless-ipv6", "hysteria2-ipv4", "hysteria2-ipv6",
		"tuic-ipv4", "tuic-ipv6", "anytls-ipv4", "anytls-ipv6",
	}
	for index, inbound := range inbounds {
		if inbound["tag"] != wantTags[index] {
			t.Fatalf("inbound[%d].tag = %v, want %q", index, inbound["tag"], wantTags[index])
		}
		wantListen := settings.ListenIPv4
		if strings.HasSuffix(wantTags[index], "ipv6") {
			wantListen = settings.ListenIPv6
		}
		if inbound["listen"] != wantListen {
			t.Fatalf("inbound[%d].listen = %v, want %q", index, inbound["listen"], wantListen)
		}
		users := objectSlice(t, inbound["users"])
		if len(users) != 2 || users[0]["name"] != "1001" || users[1]["name"] != "2002" {
			t.Fatalf("inbound[%d] users = %#v, want stable Telegram ID names", index, users)
		}
	}

	experimental := object(t, document["experimental"])
	v2rayAPI := object(t, experimental["v2ray_api"])
	stats := object(t, v2rayAPI["stats"])
	if stats["enabled"] != true {
		t.Fatalf("stats.enabled = %v, want true", stats["enabled"])
	}
	if got := stringSlice(t, stats["users"]); strings.Join(got, ",") != "1001,2002" {
		t.Fatalf("stats.users = %#v, want stable shared users", got)
	}
	if got := stringSlice(t, stats["inbounds"]); strings.Join(got, ",") != strings.Join(wantTags, ",") {
		t.Fatalf("stats.inbounds = %#v, want all inbound tags", got)
	}
}

func TestGeneratorEnforcesIPv6OnlyOutboundByDefault(t *testing.T) {
	generated, err := (Generator{}).Generate(testSettings())
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(generated, &document); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	route := object(t, document["route"])
	rules := objectSlice(t, route["rules"])
	if len(rules) == 0 || rules[0]["ip_version"] != float64(4) || rules[0]["action"] != "reject" {
		t.Fatalf("first route rule = %#v, want IPv4 rejection", rules)
	}
	outbounds := objectSlice(t, document["outbounds"])
	direct := outbounds[0]
	resolver := object(t, direct["domain_resolver"])
	if resolver["strategy"] != "ipv6_only" {
		t.Fatalf("direct domain resolver strategy = %v, want ipv6_only", resolver["strategy"])
	}
}

func TestGeneratorRejectsDuplicateUsersAndTransportPortConflicts(t *testing.T) {
	settings := testSettings()
	settings.Users = []User{{TelegramID: 1001, Credentials: testCredentialBundle("a")}, {TelegramID: 1001, Credentials: testCredentialBundle("b")}}
	if _, err := (Generator{}).Generate(settings); err == nil {
		t.Fatal("Generate() accepted duplicate Telegram IDs")
	}

	settings = testSettings()
	settings.AnyTLSPort = settings.VLESSPort
	if _, err := (Generator{}).Generate(settings); err == nil {
		t.Fatal("Generate() accepted conflicting TCP ports")
	}
	settings = testSettings()
	settings.TUICPort = settings.Hysteria2Port
	if _, err := (Generator{}).Generate(settings); err == nil {
		t.Fatal("Generate() accepted conflicting UDP ports")
	}
}

func TestGeneratorBuildsSingleStackConfigurationsWithoutPhantomInbounds(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(*Settings)
		wantSuffix string
	}{
		{
			name: "IPv4 only",
			configure: func(settings *Settings) {
				settings.ListenIPv6 = ""
			},
			wantSuffix: "-ipv4",
		},
		{
			name: "IPv6 only",
			configure: func(settings *Settings) {
				settings.ListenIPv4 = ""
			},
			wantSuffix: "-ipv6",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := testSettings()
			test.configure(&settings)
			generated, err := (Generator{}).Generate(settings)
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}
			var document map[string]any
			if err := json.Unmarshal(generated, &document); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			inbounds := objectSlice(t, document["inbounds"])
			if len(inbounds) != 4 {
				t.Fatalf("inbound count = %d, want 4", len(inbounds))
			}
			for _, inbound := range inbounds {
				tag, _ := inbound["tag"].(string)
				if !strings.HasSuffix(tag, test.wantSuffix) {
					t.Fatalf("inbound tag = %q, want suffix %q", tag, test.wantSuffix)
				}
			}
		})
	}
}

func TestGeneratorAllowsEmptyUserSetAndEnablesIPv4OutboundExplicitly(t *testing.T) {
	settings := testSettings()
	settings.Users = nil
	settings.AllowIPv4Outbound = true

	generated, err := (Generator{}).Generate(settings)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(generated, &document); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	for _, inbound := range objectSlice(t, document["inbounds"]) {
		if users := objectSlice(t, inbound["users"]); len(users) != 0 {
			t.Fatalf("inbound users = %#v, want empty", users)
		}
	}
	rules := objectSlice(t, object(t, document["route"])["rules"])
	for _, rule := range rules {
		if rule["ip_version"] == float64(4) && rule["action"] == "reject" {
			t.Fatalf("route rules = %#v, contain IPv4 rejection", rules)
		}
	}
	resolver := object(t, objectSlice(t, document["outbounds"])[0]["domain_resolver"])
	if resolver["strategy"] != "prefer_ipv6" {
		t.Fatalf("resolver strategy = %v, want prefer_ipv6", resolver["strategy"])
	}
}

func TestGeneratorRejectsMappedAddressesAndBlankProtocolSecrets(t *testing.T) {
	settings := testSettings()
	settings.ListenIPv6 = "::ffff:192.0.2.10"
	if _, err := (Generator{}).Generate(settings); err == nil {
		t.Fatal("Generate() accepted an IPv4-mapped address as an explicit IPv6 listen address")
	}

	settings = testSettings()
	settings.Users[0].Credentials.Hysteria2Password = "  \t"
	if _, err := (Generator{}).Generate(settings); err == nil {
		t.Fatal("Generate() accepted a blank protocol password")
	}
}

func TestGeneratorRejectsInvalidStatsAndCredentialFields(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*Settings)
	}{
		{name: "non-loopback stats", configure: func(settings *Settings) { settings.StatsListen = "203.0.113.10:10085" }},
		{name: "zero stats port", configure: func(settings *Settings) { settings.StatsListen = "127.0.0.1:0" }},
		{name: "wrong IPv4 family", configure: func(settings *Settings) { settings.ListenIPv4 = "2001:db8::10" }},
		{name: "invalid VLESS UUID", configure: func(settings *Settings) { settings.Users[0].Credentials.VLESSUUID = "not-a-uuid" }},
		{name: "noncanonical token", configure: func(settings *Settings) { settings.Users[0].Credentials.SubscriptionToken += "=" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := testSettings()
			test.configure(&settings)
			if _, err := (Generator{}).Generate(settings); err == nil {
				t.Fatal("Generate() error = nil")
			}
		})
	}
}

func testSettings() Settings {
	return Settings{
		ListenIPv4:         "203.0.113.10",
		ListenIPv6:         "2001:db8::10",
		VLESSPort:          443,
		Hysteria2Port:      443,
		TUICPort:           8443,
		AnyTLSPort:         8443,
		TLSServerName:      "vpn.example.com",
		TLSCertificatePath: "/run/tls/fullchain.pem",
		TLSKeyPath:         "/run/tls/privkey.pem",
		RealityServer:      "www.example.com",
		RealityServerPort:  443,
		RealityPrivateKey:  "private-key",
		RealityShortID:     "0123456789abcdef",
		StatsListen:        "127.0.0.1:10085",
		AllowIPv4Outbound:  false,
		Users:              []User{{TelegramID: 1001, Credentials: testCredentialBundle("a")}},
	}
}

func testCredentialBundle(suffix string) domain.CredentialBundle {
	return domain.CredentialBundle{
		SubscriptionToken: base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat(suffix, 32))),
		VLESSUUID:         "11111111-1111-4111-8111-11111111111" + suffix,
		Hysteria2Password: "hysteria2-" + suffix,
		TUICUUID:          "22222222-2222-4222-8222-22222222222" + suffix,
		TUICPassword:      "tuic-" + suffix,
		AnyTLSPassword:    "anytls-" + suffix,
	}
}

func object(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value = %#v, want object", value)
	}
	return result
}

func objectSlice(t *testing.T, value any) []map[string]any {
	t.Helper()
	values, ok := value.([]any)
	if !ok {
		t.Fatalf("value = %#v, want array", value)
	}
	result := make([]map[string]any, len(values))
	for index, item := range values {
		result[index] = object(t, item)
	}
	return result
}

func stringSlice(t *testing.T, value any) []string {
	t.Helper()
	values, ok := value.([]any)
	if !ok {
		t.Fatalf("value = %#v, want string array", value)
	}
	result := make([]string, len(values))
	for index, item := range values {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("value[%d] = %#v, want string", index, item)
		}
		result[index] = text
	}
	return result
}
