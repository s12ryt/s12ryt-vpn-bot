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

func TestManagementListsUsersForAdministrator(t *testing.T) {
	users := &managementStub{users: []domain.UserOverview{{TelegramID: 12345, Status: domain.AccessStatusActive}}}
	handler := NewApplicationHandler(readinessStub{}, &loginExchangerStub{}, &sessionManagerStub{administrator: auth.Administrator{TelegramID: 77, Role: auth.RoleAdministrator, Active: true}}, LoginProtection{}, &subscriptionStub{}, users, &provisioningManagementStub{})
	request := httptest.NewRequest(http.MethodGet, "/api/users?limit=50", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"telegram_id":12345`) {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func TestManagementRevokeRequiresCSRFAndPassesActor(t *testing.T) {
	users := &managementStub{}
	handler := NewApplicationHandler(readinessStub{}, &loginExchangerStub{}, &sessionManagerStub{administrator: auth.Administrator{TelegramID: 77, Role: auth.RoleAdministrator, Active: true}}, LoginProtection{}, &subscriptionStub{}, users, &provisioningManagementStub{})
	request := httptest.NewRequest(http.MethodPost, "/api/users/12345/revoke", strings.NewReader(`{"mode":"permanent_block"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeaderName, testCSRFToken)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: testCSRFToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || users.actorID != 77 || users.telegramID != 12345 || users.mode != domain.RevocationModePermanentBlock {
		t.Fatalf("response=%d actor=%d target=%d mode=%q", response.Code, users.actorID, users.telegramID, users.mode)
	}
}

func TestManagementMutationRejectsMissingCSRF(t *testing.T) {
	users := &managementStub{}
	handler := NewApplicationHandler(readinessStub{}, &loginExchangerStub{}, &sessionManagerStub{administrator: auth.Administrator{TelegramID: 77, Role: auth.RoleAdministrator, Active: true}}, LoginProtection{}, &subscriptionStub{}, users, &provisioningManagementStub{})
	request := httptest.NewRequest(http.MethodPost, "/api/users/12345/reject", strings.NewReader(`{}`))
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || users.telegramID != 0 {
		t.Fatalf("response=%d target=%d", response.Code, users.telegramID)
	}
}

type managementStub struct {
	users      []domain.UserOverview
	actorID    int64
	telegramID int64
	mode       domain.RevocationMode
}

func (stub *managementStub) ListUsers(context.Context, int64, int) ([]domain.UserOverview, error) {
	return stub.users, nil
}
func (stub *managementStub) Revoke(_ context.Context, actorID, telegramID int64, mode domain.RevocationMode, _ time.Time) error {
	stub.actorID, stub.telegramID, stub.mode = actorID, telegramID, mode
	return nil
}
func (stub *managementStub) RejectApproval(_ context.Context, actorID, telegramID int64, _ time.Time) error {
	stub.actorID, stub.telegramID = actorID, telegramID
	return nil
}

type provisioningManagementStub struct{}

func (*provisioningManagementStub) Approve(context.Context, int64, time.Time) (domain.ProvisionedAccess, error) {
	return domain.ProvisionedAccess{}, nil
}
func (*provisioningManagementStub) Rotate(context.Context, int64, time.Time, bool) (domain.ProvisionedAccess, error) {
	return domain.ProvisionedAccess{}, nil
}
