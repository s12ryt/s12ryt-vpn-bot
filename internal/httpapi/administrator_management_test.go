package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
)

func TestOwnerListsAdministrators(t *testing.T) {
	administrators := &administratorManagementStub{items: []auth.Administrator{{TelegramID: 77, Role: auth.RoleOwner, Root: true, Active: true}}}
	handler := NewApplicationHandler(
		readinessStub{}, &loginExchangerStub{},
		&sessionManagerStub{administrator: auth.Administrator{TelegramID: 77, Role: auth.RoleOwner, Root: true, Active: true}},
		LoginProtection{}, &subscriptionStub{}, &managementStub{}, &provisioningManagementStub{}, administrators,
	)
	request := httptest.NewRequest(http.MethodGet, "/api/administrators", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"telegram_id":77`) || !strings.Contains(response.Body.String(), `"role":"owner"`) {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func TestOwnerSetsAdministratorRoleWithCSRF(t *testing.T) {
	administrators := &administratorManagementStub{}
	handler := administratorTestHandler(auth.RoleOwner, administrators)
	request := httptest.NewRequest(http.MethodPost, "/api/administrators/12345/role", strings.NewReader(`{"role":"administrator"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeaderName, testCSRFToken)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || administrators.actorID != 77 || administrators.targetID != 12345 || administrators.role != auth.RoleAdministrator {
		t.Fatalf("response=%d actor=%d target=%d role=%q", response.Code, administrators.actorID, administrators.targetID, administrators.role)
	}
}

func TestOwnerRemovesAdministratorAndAdministratorCannotManageRoles(t *testing.T) {
	administrators := &administratorManagementStub{}
	ownerHandler := administratorTestHandler(auth.RoleOwner, administrators)
	request := httptest.NewRequest(http.MethodDelete, "/api/administrators/12345", nil)
	request.Header.Set(csrfHeaderName, testCSRFToken)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
	response := httptest.NewRecorder()
	ownerHandler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || administrators.targetID != 12345 {
		t.Fatalf("owner response=%d target=%d", response.Code, administrators.targetID)
	}

	administrators.targetID = 0
	adminHandler := administratorTestHandler(auth.RoleAdministrator, administrators)
	request = httptest.NewRequest(http.MethodDelete, "/api/administrators/12345", nil)
	request.Header.Set(csrfHeaderName, testCSRFToken)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
	response = httptest.NewRecorder()
	adminHandler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || administrators.targetID != 0 {
		t.Fatalf("administrator response=%d target=%d", response.Code, administrators.targetID)
	}
}

func TestAdministratorRoleMutationRequiresCSRFAndCanonicalTarget(t *testing.T) {
	administrators := &administratorManagementStub{}
	handler := administratorTestHandler(auth.RoleOwner, administrators)
	request := httptest.NewRequest(http.MethodPost, "/api/administrators/123/role", strings.NewReader(`{"role":"owner"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || administrators.targetID != 0 {
		t.Fatalf("missing CSRF response=%d target=%d", response.Code, administrators.targetID)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/administrators/0123/role", strings.NewReader(`{"role":"owner"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeaderName, testCSRFToken)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || administrators.targetID != 0 {
		t.Fatalf("noncanonical response=%d target=%d", response.Code, administrators.targetID)
	}
}

func administratorTestHandler(role auth.Role, administrators *administratorManagementStub) http.Handler {
	return NewApplicationHandler(
		readinessStub{}, &loginExchangerStub{},
		&sessionManagerStub{administrator: auth.Administrator{TelegramID: 77, Role: role, Active: true}},
		LoginProtection{}, &subscriptionStub{}, &managementStub{}, &provisioningManagementStub{}, administrators,
	)
}

type administratorManagementStub struct {
	items    []auth.Administrator
	actorID  int64
	targetID int64
	role     auth.Role
}

func (stub *administratorManagementStub) List(context.Context) ([]auth.Administrator, error) {
	return stub.items, nil
}

func (stub *administratorManagementStub) SetRole(_ context.Context, actorID, targetID int64, role auth.Role, _ time.Time) error {
	stub.actorID, stub.targetID, stub.role = actorID, targetID, role
	return nil
}

func (stub *administratorManagementStub) Remove(_ context.Context, actorID, targetID int64, _ time.Time) error {
	stub.actorID, stub.targetID = actorID, targetID
	return nil
}
