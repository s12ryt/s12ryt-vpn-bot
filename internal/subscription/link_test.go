package subscription

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestLinkBuilderPreservesPublicURLPathPrefix(t *testing.T) {
	builder, err := NewLinkBuilder("https://vpn.example.com/admin")
	if err != nil {
		t.Fatalf("NewLinkBuilder() error = %v", err)
	}
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))

	link, err := builder.SubscriptionURL(token)
	if err != nil {
		t.Fatalf("SubscriptionURL() error = %v", err)
	}
	want := "https://vpn.example.com/admin/sub/" + token
	if link != want {
		t.Fatalf("SubscriptionURL() = %q, want %q", link, want)
	}
}

func TestLinkBuilderRejectsUnsafeBaseURLAndMalformedToken(t *testing.T) {
	for _, baseURL := range []string{"", "http://vpn.example.com", "https://user@vpn.example.com", "https://vpn.example.com?x=1"} {
		if _, err := NewLinkBuilder(baseURL); err == nil {
			t.Fatalf("NewLinkBuilder(%q) error = nil", baseURL)
		}
	}
	builder, err := NewLinkBuilder("https://vpn.example.com")
	if err != nil {
		t.Fatalf("NewLinkBuilder() error = %v", err)
	}
	for _, token := range []string{"", strings.Repeat("A", 42), strings.Repeat("/", 43), strings.Repeat("A", 44)} {
		if _, err := builder.SubscriptionURL(token); err == nil {
			t.Fatalf("SubscriptionURL(%q) error = nil", token)
		}
	}
}
