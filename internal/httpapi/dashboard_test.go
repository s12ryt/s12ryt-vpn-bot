package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

func TestAdministratorReadsOperationalOverview(t *testing.T) {
	provider := &dashboardProviderStub{
		statistics: domain.UserStatistics{Total: 1200, Active: 900, Pending: 15, Blocked: 4, TotalUsedBytes: 4_500_000_000_000},
		tlsIssued:  true, coreConfigured: true,
	}
	handler := dashboardTestHandler(auth.RoleAdministrator, provider)

	request := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("response=%d %q", response.Code, response.Body.String())
	}
	for _, required := range []string{`"total_users":1200`, `"active_users":900`, `"pending_approvals":15`, `"blocked_users":4`, `"tls_issued":true`, `"core_configured":true`} {
		if !strings.Contains(response.Body.String(), required) {
			t.Fatalf("overview missing %q: %q", required, response.Body.String())
		}
	}
}

func TestOverviewWithoutSessionIsRejected(t *testing.T) {
	provider := &dashboardProviderStub{}
	handler := dashboardTestHandler(auth.RoleAdministrator, provider)

	request := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized || provider.called {
		t.Fatalf("response=%d called=%v", response.Code, provider.called)
	}
}

func dashboardTestHandler(role auth.Role, provider DashboardProvider) http.Handler {
	return NewApplicationHandler(
		readinessStub{}, &loginExchangerStub{},
		&sessionManagerStub{administrator: auth.Administrator{TelegramID: 77, Role: role, Active: true}},
		LoginProtection{}, &subscriptionStub{}, &managementStub{}, &provisioningManagementStub{}, provider,
	)
}

type dashboardProviderStub struct {
	statistics     domain.UserStatistics
	tlsIssued      bool
	coreConfigured bool
	called         bool
}

func (stub *dashboardProviderStub) Statistics(context.Context) (domain.UserStatistics, error) {
	stub.called = true
	return stub.statistics, nil
}

func (stub *dashboardProviderStub) TLSIssued(context.Context) (bool, error) {
	return stub.tlsIssued, nil
}

func (stub *dashboardProviderStub) CoreConfigured(context.Context) (bool, error) {
	return stub.coreConfigured, nil
}
