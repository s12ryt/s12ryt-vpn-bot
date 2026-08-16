package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

func TestAdministratorListsAuditEvents(t *testing.T) {
	actor := int64(77)
	audits := &auditReaderStub{events: []domain.AuditEvent{{ID: 9, ActorTelegramID: &actor, Action: "vpn.revoke", TargetType: "vpn_user", TargetID: "123", Details: json.RawMessage(`{"mode":"requires_approval"}`), CreatedAt: time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)}}}
	handler := auditTestHandler(audits)
	request := httptest.NewRequest(http.MethodGet, "/api/audit?before=10&limit=20", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || audits.before != 10 || audits.limit != 20 || !strings.Contains(response.Body.String(), `"action":"vpn.revoke"`) {
		t.Fatalf("response=%d %q before=%d limit=%d", response.Code, response.Body.String(), audits.before, audits.limit)
	}
}

func TestAuditListRejectsInvalidCursorBeforeStore(t *testing.T) {
	audits := &auditReaderStub{}
	handler := auditTestHandler(audits)
	request := httptest.NewRequest(http.MethodGet, "/api/audit?before=-1", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || audits.called {
		t.Fatalf("response=%d called=%v", response.Code, audits.called)
	}
}

func auditTestHandler(audits AuditReader) http.Handler {
	return NewApplicationHandler(
		readinessStub{}, &loginExchangerStub{},
		&sessionManagerStub{administrator: auth.Administrator{TelegramID: 77, Role: auth.RoleAdministrator, Active: true}},
		LoginProtection{}, &subscriptionStub{}, &managementStub{}, &provisioningManagementStub{}, &administratorManagementStub{}, audits,
	)
}

type auditReaderStub struct {
	events []domain.AuditEvent
	before int64
	limit  int
	called bool
}

func (stub *auditReaderStub) List(_ context.Context, before int64, limit int) ([]domain.AuditEvent, error) {
	stub.before, stub.limit, stub.called = before, limit, true
	return stub.events, nil
}
