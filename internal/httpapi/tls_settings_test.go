package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

func TestOwnerReadsTLSSettingsWithoutTokenMaterial(t *testing.T) {
	manager := &tlsSettingsManagerStub{overview: domain.TLSSettingsOverview{
		Configured: true, Mode: "duckdns", Domain: "node.duckdns.org", Challenge: "dns_01",
		Email: "owner@example.com", CADirectoryURLs: []string{"https://ca.example/directory"},
		TermsAccepted: true, HasDuckDNSToken: true, State: "issued",
		CertificateExpiresAt: time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC), LastIssuedCA: "https://ca.example/directory",
	}}
	handler := tlsSettingsTestHandler(auth.RoleOwner, manager)

	request := httptest.NewRequest(http.MethodGet, "/api/settings/tls", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"mode":"duckdns"`) ||
		!strings.Contains(response.Body.String(), `"state":"issued"`) || !strings.Contains(response.Body.String(), `"has_duckdns_token":true`) {
		t.Fatalf("response=%d %q", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), `"duckdns_token":`) {
		t.Fatalf("response must not expose token material: %q", response.Body.String())
	}
}

func TestOwnerUpdatesTLSSettingsWithCSRF(t *testing.T) {
	manager := &tlsSettingsManagerStub{}
	handler := tlsSettingsTestHandler(auth.RoleOwner, manager)
	body := `{"mode":"duckdns","domain":"node.duckdns.org","challenge":"dns_01","email":"owner@example.com","ca_directory_urls":["https://ca.example/directory"],"terms_accepted":true,"duckdns_token":"duckdns-token"}`

	request := httptest.NewRequest(http.MethodPut, "/api/settings/tls", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	addManagementCSRF(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || manager.updateActor != 77 || manager.update.Mode != "duckdns" ||
		manager.update.DuckDNSToken != "duckdns-token" || !manager.update.TermsAccepted {
		t.Fatalf("response=%d actor=%d update=%#v", response.Code, manager.updateActor, manager.update)
	}
}

func TestOwnerUpdateTLSRejectsUnknownFields(t *testing.T) {
	manager := &tlsSettingsManagerStub{}
	handler := tlsSettingsTestHandler(auth.RoleOwner, manager)
	body := `{"mode":"custom","domain":"vpn.example.com","challenge":"http_01","terms_accepted":true,"ca_directory_urls":["https://ca.example/directory"],"has_duckdns_token":false}`

	request := httptest.NewRequest(http.MethodPut, "/api/settings/tls", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	addManagementCSRF(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || manager.updateCalled {
		t.Fatalf("response=%d called=%v", response.Code, manager.updateCalled)
	}
}

func TestAdministratorCannotManageTLSSettings(t *testing.T) {
	manager := &tlsSettingsManagerStub{}
	handler := tlsSettingsTestHandler(auth.RoleAdministrator, manager)

	request := httptest.NewRequest(http.MethodGet, "/api/settings/tls", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || manager.overviewCalled {
		t.Fatalf("GET response=%d called=%v", response.Code, manager.overviewCalled)
	}

	update := httptest.NewRequest(http.MethodPut, "/api/settings/tls", strings.NewReader(`{"mode":"custom","domain":"vpn.example.com","challenge":"http_01","terms_accepted":true,"ca_directory_urls":["https://ca.example/directory"]}`))
	update.Header.Set("Content-Type", "application/json")
	addManagementCSRF(update)
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, update)
	if updateResponse.Code != http.StatusForbidden || manager.updateCalled {
		t.Fatalf("PUT response=%d called=%v", updateResponse.Code, manager.updateCalled)
	}
}

func tlsSettingsTestHandler(role auth.Role, manager TLSSettingsManager) http.Handler {
	return NewApplicationHandler(
		readinessStub{}, &loginExchangerStub{},
		&sessionManagerStub{administrator: auth.Administrator{TelegramID: 77, Role: role, Active: true}},
		LoginProtection{}, &subscriptionStub{}, &managementStub{}, &provisioningManagementStub{}, &managementSettingsStub{}, manager,
	)
}

type tlsSettingsManagerStub struct {
	overview       domain.TLSSettingsOverview
	overviewCalled bool
	update         domain.TLSSettingsUpdate
	updateActor    int64
	updateCalled   bool
}

func (stub *tlsSettingsManagerStub) GetOverview(context.Context) (domain.TLSSettingsOverview, error) {
	stub.overviewCalled = true
	return stub.overview, nil
}

func (stub *tlsSettingsManagerStub) Save(_ context.Context, actor int64, update domain.TLSSettingsUpdate, _ time.Time) error {
	stub.updateCalled, stub.updateActor, stub.update = true, actor, update
	return nil
}
