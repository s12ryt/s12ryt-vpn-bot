package httpapi

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestSourceIPIgnoresForwardingHeadersFromUntrustedPeer(t *testing.T) {
	resolver := NewSourceIPResolver([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "203.0.113.10:4321"
	request.Header.Set("Forwarded", "for=198.51.100.7")
	request.Header.Set("X-Forwarded-For", "198.51.100.8")

	if got := resolver.Resolve(request); got != netip.MustParseAddr("203.0.113.10") {
		t.Fatalf("Resolve() = %v, want direct peer", got)
	}
}

func TestSourceIPUsesForwardedThenXForwardedForFromTrustedPeer(t *testing.T) {
	resolver := NewSourceIPResolver([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})
	tests := []struct {
		name      string
		forwarded string
		xff       string
		want      string
	}{
		{name: "standard Forwarded", forwarded: `for="[2001:db8::5]:4711";proto=https`, xff: "198.51.100.8", want: "2001:db8::5"},
		{name: "X-Forwarded-For fallback", xff: "198.51.100.8, 10.0.0.9", want: "198.51.100.8"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/", nil)
			request.RemoteAddr = "10.0.0.2:4321"
			request.Header.Set("Forwarded", test.forwarded)
			request.Header.Set("X-Forwarded-For", test.xff)

			if got := resolver.Resolve(request); got != netip.MustParseAddr(test.want) {
				t.Fatalf("Resolve() = %v, want %s", got, test.want)
			}
		})
	}
}

func TestSourceIPFallsBackToTrustedPeerWhenForwardingHeaderIsMalformed(t *testing.T) {
	resolver := NewSourceIPResolver([]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "10.0.0.2:4321"
	request.Header.Set("Forwarded", "for=not-an-ip")
	request.Header.Set("X-Forwarded-For", "198.51.100.8")

	if got := resolver.Resolve(request); got != netip.MustParseAddr("10.0.0.2") {
		t.Fatalf("Resolve() = %v, want trusted peer fallback", got)
	}
}
