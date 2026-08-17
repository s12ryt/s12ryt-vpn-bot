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

func TestOwnerReadsBotSettingsWithoutTokenMaterial(t *testing.T) {
	manager := &botSettingsManagerStub{overview: BotSettingsOverview{
		BotUsername: "member_bot", UpdatedAt: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC),
	}}
	handler := botSettingsTestHandler(auth.RoleOwner, manager)

	request := httptest.NewRequest(http.MethodGet, "/api/settings/bot", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"bot_username":"member_bot"`) {
		t.Fatalf("response=%d %q", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "token") {
		t.Fatalf("response must not expose token fields: %q", response.Body.String())
	}
}

func TestOwnerRotatesBotTokenWithCSRF(t *testing.T) {
	manager := &botSettingsManagerStub{}
	handler := botSettingsTestHandler(auth.RoleOwner, manager)

	request := httptest.NewRequest(http.MethodPut, "/api/settings/bot", strings.NewReader(`{"bot_token":"rotated-token"}`))
	request.Header.Set("Content-Type", "application/json")
	addManagementCSRF(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || manager.rotateActor != 77 || manager.rotateToken != "rotated-token" {
		t.Fatalf("response=%d actor=%d token=%q", response.Code, manager.rotateActor, manager.rotateToken)
	}
}

func TestBotTokenRotationRejectsEmptyTokenAndUnknownFields(t *testing.T) {
	manager := &botSettingsManagerStub{}
	handler := botSettingsTestHandler(auth.RoleOwner, manager)

	empty := httptest.NewRequest(http.MethodPut, "/api/settings/bot", strings.NewReader(`{"bot_token":""}`))
	empty.Header.Set("Content-Type", "application/json")
	addManagementCSRF(empty)
	emptyResponse := httptest.NewRecorder()
	handler.ServeHTTP(emptyResponse, empty)
	if emptyResponse.Code != http.StatusBadRequest || manager.rotateCalled {
		t.Fatalf("empty token response=%d called=%v", emptyResponse.Code, manager.rotateCalled)
	}

	unknown := httptest.NewRequest(http.MethodPut, "/api/settings/bot", strings.NewReader(`{"bot_token":"t","bot_username":"x"}`))
	unknown.Header.Set("Content-Type", "application/json")
	addManagementCSRF(unknown)
	unknownResponse := httptest.NewRecorder()
	handler.ServeHTTP(unknownResponse, unknown)
	if unknownResponse.Code != http.StatusBadRequest || manager.rotateCalled {
		t.Fatalf("unknown field response=%d called=%v", unknownResponse.Code, manager.rotateCalled)
	}
}

func TestBotTokenRotationRequiresCSRF(t *testing.T) {
	manager := &botSettingsManagerStub{}
	handler := botSettingsTestHandler(auth.RoleOwner, manager)

	request := httptest.NewRequest(http.MethodPut, "/api/settings/bot", strings.NewReader(`{"bot_token":"t"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || manager.rotateCalled {
		t.Fatalf("response=%d called=%v", response.Code, manager.rotateCalled)
	}
}

func TestAdministratorCannotRotateBotToken(t *testing.T) {
	manager := &botSettingsManagerStub{}
	handler := botSettingsTestHandler(auth.RoleAdministrator, manager)

	request := httptest.NewRequest(http.MethodPut, "/api/settings/bot", strings.NewReader(`{"bot_token":"t"}`))
	request.Header.Set("Content-Type", "application/json")
	addManagementCSRF(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || manager.rotateCalled {
		t.Fatalf("response=%d called=%v", response.Code, manager.rotateCalled)
	}
}

func botSettingsTestHandler(role auth.Role, manager BotSettingsManager) http.Handler {
	return NewApplicationHandler(
		readinessStub{}, &loginExchangerStub{},
		&sessionManagerStub{administrator: auth.Administrator{TelegramID: 77, Role: role, Active: true}},
		LoginProtection{}, &subscriptionStub{}, &managementStub{}, &provisioningManagementStub{}, manager,
	)
}

type botSettingsManagerStub struct {
	overview     BotSettingsOverview
	overviewSeen bool
	rotateActor  int64
	rotateToken  string
	rotateCalled bool
	rotateErr    error
}

func (stub *botSettingsManagerStub) GetOverview(context.Context) (BotSettingsOverview, error) {
	stub.overviewSeen = true
	return stub.overview, nil
}

func (stub *botSettingsManagerStub) Rotate(_ context.Context, actor int64, token string, _ time.Time) error {
	stub.rotateCalled, stub.rotateActor, stub.rotateToken = true, actor, token
	return stub.rotateErr
}
