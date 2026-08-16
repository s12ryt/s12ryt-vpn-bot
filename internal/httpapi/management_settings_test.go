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

func TestOwnerReadsManagementSettings(t *testing.T) {
	manager := &managementSettingsStub{settings: validHTTPManagementSettings(), rules: []domain.QualificationRuleOverview{{ChatID: -1001, ChatType: "supergroup", Enabled: true, BotAdministratorPassed: true}}}
	handler := managementSettingsTestHandler(auth.RoleOwner, manager)
	request := httptest.NewRequest(http.MethodGet, "/api/settings/management", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"qualification_mode":"any"`) || !strings.Contains(response.Body.String(), `"chat_id":-1001`) {
		t.Fatalf("response=%d %q", response.Code, response.Body.String())
	}
}

func TestOwnerUpdatesManagementSettingsWithCSRF(t *testing.T) {
	manager := &managementSettingsStub{}
	handler := managementSettingsTestHandler(auth.RoleOwner, manager)
	body := `{"qualification_mode":"all","recheck_interval_minutes":120,"recheck_requests_per_second":12,"recheck_batch_size":80,"inactivity_threshold_days":7,"quota_limit_bytes":40000000000,"confirm_inactivity_removal":true}`
	request := httptest.NewRequest(http.MethodPut, "/api/settings/management", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	addManagementCSRF(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || manager.actor != 77 || manager.settings.QualificationMode != domain.QualificationAll || !manager.confirm {
		t.Fatalf("response=%d actor=%d settings=%#v confirm=%v", response.Code, manager.actor, manager.settings, manager.confirm)
	}
}

func TestOwnerPreviewsInactivityRemoval(t *testing.T) {
	manager := &managementSettingsStub{previewCount: 12}
	handler := managementSettingsTestHandler(auth.RoleOwner, manager)
	request := httptest.NewRequest(http.MethodPost, "/api/settings/management/inactivity-preview", strings.NewReader(`{"threshold_days":7}`))
	request.Header.Set("Content-Type", "application/json")
	addManagementCSRF(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || manager.previewDays != 7 || !strings.Contains(response.Body.String(), `"affected_users":12`) {
		t.Fatalf("response=%d %q days=%d", response.Code, response.Body.String(), manager.previewDays)
	}
}

func TestAdministratorCannotManageGlobalSettings(t *testing.T) {
	manager := &managementSettingsStub{}
	handler := managementSettingsTestHandler(auth.RoleAdministrator, manager)
	request := httptest.NewRequest(http.MethodGet, "/api/settings/management", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || manager.called {
		t.Fatalf("response=%d called=%v", response.Code, manager.called)
	}
}

func managementSettingsTestHandler(role auth.Role, manager ManagementSettingsManager) http.Handler {
	return NewApplicationHandler(
		readinessStub{}, &loginExchangerStub{},
		&sessionManagerStub{administrator: auth.Administrator{TelegramID: 77, Role: role, Active: true}},
		LoginProtection{}, &subscriptionStub{}, &managementStub{}, &provisioningManagementStub{}, manager,
	)
}

func addManagementCSRF(request *http.Request) {
	token := strings.Repeat("A", 43)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
	request.Header.Set(csrfHeaderName, token)
}

func validHTTPManagementSettings() domain.ManagementSettings {
	return domain.ManagementSettings{QualificationMode: domain.QualificationAny, RecheckIntervalMinutes: 60, RecheckRequestsPerSecond: 10, RecheckBatchSize: 50, QuotaLimitBytes: 50_000_000_000}
}

type managementSettingsStub struct {
	settings     domain.ManagementSettings
	rules        []domain.QualificationRuleOverview
	previewCount int64
	previewDays  int
	actor        int64
	confirm      bool
	called       bool
}

func (stub *managementSettingsStub) Get(context.Context) (domain.ManagementSettings, []domain.QualificationRuleOverview, error) {
	stub.called = true
	return stub.settings, stub.rules, nil
}

func (stub *managementSettingsStub) PreviewInactivity(_ context.Context, thresholdDays int, _ time.Time) (int64, error) {
	stub.called, stub.previewDays = true, thresholdDays
	return stub.previewCount, nil
}

func (stub *managementSettingsStub) Update(_ context.Context, actor int64, settings domain.ManagementSettings, confirm bool, _ time.Time) error {
	stub.called, stub.actor, stub.settings, stub.confirm = true, actor, settings, confirm
	return nil
}
