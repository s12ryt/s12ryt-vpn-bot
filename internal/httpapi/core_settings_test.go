package httpapi

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

func TestOwnerReadsCoreSettingsWithoutPrivateKey(t *testing.T) {
	manager := &coreSettingsManagerStub{overview: validHTTPCoreOverview()}
	handler := coreSettingsTestHandler(auth.RoleOwner, manager)
	request := httptest.NewRequest(http.MethodGet, "/api/settings/core", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"listen_ipv6":"2001:db8::10"`) ||
		!strings.Contains(response.Body.String(), `"has_reality_private_key":true`) || strings.Contains(response.Body.String(), `"reality_private_key":`) {
		t.Fatalf("response=%d %q", response.Code, response.Body.String())
	}
}

func TestOwnerUpdatesCoreSettingsWithWriteOnlySecretAndCSRF(t *testing.T) {
	manager := &coreSettingsManagerStub{}
	handler := coreSettingsTestHandler(auth.RoleOwner, manager)
	privateKey := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	body := `{"configured":true,"listen_ipv4":"203.0.113.10","listen_ipv6":"2001:db8::10","vless_port":443,"hysteria2_port":443,"tuic_port":8443,"anytls_port":8443,"tls_server_name":"vpn.example.com","tls_certificate_path":"/run/tls/fullchain.pem","tls_key_path":"/run/tls/privkey.pem","reality_server":"www.example.com","reality_server_port":443,"reality_short_id":"0123456789abcdef","stats_listen":"127.0.0.1:10085","allow_ipv4_outbound":false,"reality_private_key":"` + privateKey + `"}`
	request := httptest.NewRequest(http.MethodPut, "/api/settings/core", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	addManagementCSRF(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || manager.actor != 77 || manager.update.RealityPrivateKey != privateKey || manager.update.ListenIPv4 != "203.0.113.10" {
		t.Fatalf("response=%d actor=%d update=%#v", response.Code, manager.actor, manager.update)
	}
}

func TestAdministratorCannotManageCoreSettings(t *testing.T) {
	manager := &coreSettingsManagerStub{}
	handler := coreSettingsTestHandler(auth.RoleAdministrator, manager)
	request := httptest.NewRequest(http.MethodGet, "/api/settings/core", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || manager.called {
		t.Fatalf("response=%d called=%v", response.Code, manager.called)
	}
}

func coreSettingsTestHandler(role auth.Role, manager CoreSettingsManager) http.Handler {
	return NewApplicationHandler(
		readinessStub{}, &loginExchangerStub{},
		&sessionManagerStub{administrator: auth.Administrator{TelegramID: 77, Role: role, Active: true}},
		LoginProtection{}, &subscriptionStub{}, &managementStub{}, &provisioningManagementStub{}, manager,
	)
}

func validHTTPCoreOverview() domain.CoreSettingsOverview {
	return domain.CoreSettingsOverview{
		Configured: true, ListenIPv4: "203.0.113.10", ListenIPv6: "2001:db8::10",
		VLESSPort: 443, Hysteria2Port: 443, TUICPort: 8443, AnyTLSPort: 8443,
		TLSServerName: "vpn.example.com", TLSCertificatePath: "/run/tls/fullchain.pem", TLSKeyPath: "/run/tls/privkey.pem",
		RealityServer: "www.example.com", RealityServerPort: 443, RealityShortID: "0123456789abcdef",
		StatsListen: "127.0.0.1:10085", HasRealityPrivateKey: true,
	}
}

type coreSettingsManagerStub struct {
	overview domain.CoreSettingsOverview
	update   domain.CoreSettingsUpdate
	actor    int64
	called   bool
}

func (stub *coreSettingsManagerStub) GetCore(context.Context) (domain.CoreSettingsOverview, error) {
	stub.called = true
	return stub.overview, nil
}

func (stub *coreSettingsManagerStub) UpdateCore(_ context.Context, actor int64, update domain.CoreSettingsUpdate, _ time.Time) error {
	stub.called, stub.actor, stub.update = true, actor, update
	return nil
}
