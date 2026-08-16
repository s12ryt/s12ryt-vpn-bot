package subscription

import "testing"

func TestNegotiateFormatUsesExplicitQueryBeforeUserAgent(t *testing.T) {
	tests := []struct {
		query, userAgent string
		want             Format
	}{
		{"sing-box", "Clash.Meta", FormatSingBox},
		{"clash", "sing-box/1.12", FormatClash},
		{"base64", "sing-box/1.12", FormatBase64},
		{"", "sing-box/1.12", FormatSingBox},
		{"", "Mihomo/1.19", FormatClash},
		{"", "Clash.Meta", FormatClash},
		{"", "unknown-client", FormatBase64},
	}
	for _, test := range tests {
		got, err := NegotiateFormat(test.query, test.userAgent)
		if err != nil || got != test.want {
			t.Fatalf("NegotiateFormat(%q, %q) = %q, %v; want %q", test.query, test.userAgent, got, err, test.want)
		}
	}
}

func TestNegotiateFormatRejectsUnknownExplicitFormat(t *testing.T) {
	if _, err := NegotiateFormat("yaml", ""); err == nil {
		t.Fatal("NegotiateFormat() error = nil, want error")
	}
}
