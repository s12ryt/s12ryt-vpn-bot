package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/reality"
)

func TestOwnerTriggersRealitySearch(t *testing.T) {
	manager := &realitySearchManagerStub{}
	handler := realitySearchTestHandler(auth.RoleOwner, manager)
	request := httptest.NewRequest(http.MethodPost, "/api/settings/reality/search", nil)
	addManagementCSRF(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || !manager.started {
		t.Fatalf("response=%d %q started=%v", response.Code, response.Body.String(), manager.started)
	}
	if !strings.Contains(response.Body.String(), `"status":"running"`) {
		t.Fatalf("body=%q", response.Body.String())
	}
	if manager.ctx == nil {
		t.Fatal("Start received nil context")
	}
	if err := manager.ctx.Err(); err != nil {
		t.Fatalf("Start context already cancelled: %v", err)
	}
}

func TestOwnerPollsRealitySearchResults(t *testing.T) {
	manager := &realitySearchManagerStub{snapshot: reality.SearchSnapshot{
		Status: reality.SearchStatusCompleted,
		Targets: []reality.Target{
			{Domain: "www.example.com", TLS13: true, Latency: 42 * time.Millisecond},
		},
	}}
	handler := realitySearchTestHandler(auth.RoleOwner, manager)
	request := httptest.NewRequest(http.MethodGet, "/api/settings/reality/search", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"completed"`) ||
		!strings.Contains(response.Body.String(), `"domain":"www.example.com"`) {
		t.Fatalf("response=%d %q", response.Code, response.Body.String())
	}
}

func TestRealitySearchRejectsConcurrentRun(t *testing.T) {
	manager := &realitySearchManagerStub{startErr: reality.ErrSearchRunning}
	handler := realitySearchTestHandler(auth.RoleOwner, manager)
	request := httptest.NewRequest(http.MethodPost, "/api/settings/reality/search", nil)
	addManagementCSRF(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "reality_search_running") {
		t.Fatalf("response=%d %q", response.Code, response.Body.String())
	}
}

func TestRealitySearchRequiresCSRF(t *testing.T) {
	manager := &realitySearchManagerStub{}
	handler := realitySearchTestHandler(auth.RoleOwner, manager)
	request := httptest.NewRequest(http.MethodPost, "/api/settings/reality/search", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || manager.started {
		t.Fatalf("response=%d started=%v", response.Code, manager.started)
	}
}

func TestAdministratorCannotTriggerRealitySearch(t *testing.T) {
	manager := &realitySearchManagerStub{}
	handler := realitySearchTestHandler(auth.RoleAdministrator, manager)
	request := httptest.NewRequest(http.MethodPost, "/api/settings/reality/search", nil)
	addManagementCSRF(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || manager.started {
		t.Fatalf("response=%d started=%v", response.Code, manager.started)
	}
}

func TestRealitySearchWithoutManagerIsNotFound(t *testing.T) {
	handler := NewApplicationHandler(
		readinessStub{}, &loginExchangerStub{},
		&sessionManagerStub{administrator: auth.Administrator{TelegramID: 77, Role: auth.RoleOwner, Active: true}},
		LoginProtection{}, &subscriptionStub{}, &managementStub{}, &provisioningManagementStub{},
	)
	request := httptest.NewRequest(http.MethodPost, "/api/settings/reality/search", nil)
	addManagementCSRF(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("response=%d", response.Code)
	}
}

func realitySearchTestHandler(role auth.Role, manager RealitySearchManager) http.Handler {
	return NewApplicationHandler(
		readinessStub{}, &loginExchangerStub{},
		&sessionManagerStub{administrator: auth.Administrator{TelegramID: 77, Role: role, Active: true}},
		LoginProtection{}, &subscriptionStub{}, &managementStub{}, &provisioningManagementStub{}, manager,
	)
}

type realitySearchManagerStub struct {
	startErr error
	snapshot reality.SearchSnapshot
	started  bool
	ctx      context.Context
}

func (stub *realitySearchManagerStub) Start(ctx context.Context) error {
	stub.started = true
	stub.ctx = ctx
	return stub.startErr
}

func (stub *realitySearchManagerStub) Snapshot() reality.SearchSnapshot {
	return stub.snapshot
}
