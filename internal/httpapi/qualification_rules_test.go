package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/qualification"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
)

func TestOwnerEnablesAndDisablesQualificationRule(t *testing.T) {
	manager := &qualificationRuleManagerStub{}
	handler := qualificationRuleTestHandler(auth.RoleOwner, manager)
	request := httptest.NewRequest(http.MethodPut, "/api/settings/qualification-rules/-1001", strings.NewReader(`{"chat_type":"supergroup","title":"會員群"}`))
	request.Header.Set("Content-Type", "application/json")
	addManagementCSRF(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || manager.actor != 77 || manager.rule.ChatID != -1001 || manager.rule.ChatType != telegram.ChatSupergroup {
		t.Fatalf("enable response=%d actor=%d rule=%#v", response.Code, manager.actor, manager.rule)
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/settings/qualification-rules/-1001", nil)
	addManagementCSRF(request)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || manager.disabledChatID != -1001 {
		t.Fatalf("disable response=%d chat=%d", response.Code, manager.disabledChatID)
	}
}

func TestAdministratorCannotManageQualificationRules(t *testing.T) {
	manager := &qualificationRuleManagerStub{}
	handler := qualificationRuleTestHandler(auth.RoleAdministrator, manager)
	request := httptest.NewRequest(http.MethodDelete, "/api/settings/qualification-rules/-1001", nil)
	addManagementCSRF(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || manager.called {
		t.Fatalf("response=%d called=%v", response.Code, manager.called)
	}
}

type qualificationRuleManagerStub struct {
	called         bool
	actor          int64
	rule           qualification.ManagedRule
	disabledChatID int64
}

func (stub *qualificationRuleManagerStub) EnableByActor(_ context.Context, actor int64, rule qualification.ManagedRule) error {
	stub.called, stub.actor, stub.rule = true, actor, rule
	return nil
}

func (stub *qualificationRuleManagerStub) DisableByActor(_ context.Context, actor, chatID int64) error {
	stub.called, stub.actor, stub.disabledChatID = true, actor, chatID
	return nil
}

func qualificationRuleTestHandler(role auth.Role, manager QualificationRuleManager) http.Handler {
	return NewApplicationHandler(
		readinessStub{}, &loginExchangerStub{},
		&sessionManagerStub{administrator: auth.Administrator{TelegramID: 77, Role: role, Active: true}},
		LoginProtection{}, &subscriptionStub{}, &managementStub{}, &provisioningManagementStub{}, manager,
	)
}
