package domain

import (
	"errors"
	"io"
	"regexp"
	"testing"
)

var (
	uuidV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	tokenPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

func TestCredentialIssuerCreatesIndependentProtocolCredentials(t *testing.T) {
	issuer := NewCredentialIssuer(&incrementingReader{})

	bundle, err := issuer.Issue()
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	if !tokenPattern.MatchString(bundle.SubscriptionToken) {
		t.Fatalf("SubscriptionToken = %q, want 32-byte unpadded base64url", bundle.SubscriptionToken)
	}
	if !uuidV4Pattern.MatchString(bundle.VLESSUUID) {
		t.Fatalf("VLESSUUID = %q, want UUID v4", bundle.VLESSUUID)
	}
	if !uuidV4Pattern.MatchString(bundle.TUICUUID) {
		t.Fatalf("TUICUUID = %q, want UUID v4", bundle.TUICUUID)
	}
	if bundle.VLESSUUID == bundle.TUICUUID {
		t.Fatal("VLESSUUID and TUICUUID must be independent")
	}

	passwords := []string{bundle.Hysteria2Password, bundle.TUICPassword, bundle.AnyTLSPassword}
	seen := map[string]bool{}
	for _, password := range passwords {
		if !tokenPattern.MatchString(password) {
			t.Fatalf("password = %q, want 32-byte unpadded base64url", password)
		}
		if seen[password] {
			t.Fatal("protocol passwords must be independent")
		}
		seen[password] = true
	}
}

func TestCredentialIssuerRotationProducesEntirelyNewBundle(t *testing.T) {
	issuer := NewCredentialIssuer(&incrementingReader{})
	first, err := issuer.Issue()
	if err != nil {
		t.Fatalf("first Issue() error = %v", err)
	}
	second, err := issuer.Issue()
	if err != nil {
		t.Fatalf("second Issue() error = %v", err)
	}

	if first.SubscriptionToken == second.SubscriptionToken ||
		first.VLESSUUID == second.VLESSUUID ||
		first.Hysteria2Password == second.Hysteria2Password ||
		first.TUICUUID == second.TUICUUID ||
		first.TUICPassword == second.TUICPassword ||
		first.AnyTLSPassword == second.AnyTLSPassword {
		t.Fatal("rotation must replace every credential")
	}
}

func TestCredentialIssuerReturnsNoBundleWhenRandomSourceFails(t *testing.T) {
	issuer := NewCredentialIssuer(errorReader{})

	bundle, err := issuer.Issue()
	if err == nil {
		t.Fatal("Issue() error = nil, want random source error")
	}
	if bundle != (CredentialBundle{}) {
		t.Fatalf("Issue() bundle = %#v, want zero bundle", bundle)
	}
}

type incrementingReader struct {
	next byte
}

func (r *incrementingReader) Read(p []byte) (int, error) {
	for i := range p {
		r.next++
		p[i] = r.next
	}
	return len(p), nil
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("random source unavailable")
}

var _ io.Reader = (*incrementingReader)(nil)
