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

func TestOwnerReadsAndUpdatesBackupRetention(t *testing.T) {
	manager := &backupSettingsManagerStub{settings: domain.BackupSettings{RetentionDays: 7}}
	handler := backupSettingsTestHandler(auth.RoleOwner, manager)

	read := httptest.NewRequest(http.MethodGet, "/api/settings/backup", nil)
	read.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session"})
	readResponse := httptest.NewRecorder()
	handler.ServeHTTP(readResponse, read)
	if readResponse.Code != http.StatusOK || !strings.Contains(readResponse.Body.String(), `"retention_days":7`) {
		t.Fatalf("read response=%d %q", readResponse.Code, readResponse.Body.String())
	}

	update := httptest.NewRequest(http.MethodPut, "/api/settings/backup", strings.NewReader(`{"retention_days":14}`))
	update.Header.Set("Content-Type", "application/json")
	addManagementCSRF(update)
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, update)
	if updateResponse.Code != http.StatusNoContent || manager.actor != 77 || manager.updated.RetentionDays != 14 {
		t.Fatalf("update response=%d actor=%d settings=%#v", updateResponse.Code, manager.actor, manager.updated)
	}
}

func TestBackupSettingsRequiresOwnerAndCSRF(t *testing.T) {
	manager := &backupSettingsManagerStub{}
	handler := backupSettingsTestHandler(auth.RoleAdministrator, manager)
	request := httptest.NewRequest(http.MethodPut, "/api/settings/backup", strings.NewReader(`{"retention_days":14}`))
	request.Header.Set("Content-Type", "application/json")
	addManagementCSRF(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || manager.called {
		t.Fatalf("response=%d called=%v", response.Code, manager.called)
	}
}

func backupSettingsTestHandler(role auth.Role, manager BackupSettingsManager) http.Handler {
	return NewApplicationHandler(readinessStub{}, &loginExchangerStub{}, &sessionManagerStub{administrator: auth.Administrator{TelegramID: 77, Role: role, Active: true}}, LoginProtection{}, &subscriptionStub{}, &managementStub{}, &provisioningManagementStub{}, manager)
}

type backupSettingsManagerStub struct {
	settings domain.BackupSettings
	updated  domain.BackupSettings
	actor    int64
	called   bool
}

func (stub *backupSettingsManagerStub) GetBackupSettings(context.Context) (domain.BackupSettings, error) {
	return stub.settings, nil
}

func (stub *backupSettingsManagerStub) UpdateBackupSettings(_ context.Context, actor int64, settings domain.BackupSettings, _ time.Time) error {
	stub.called, stub.actor, stub.updated = true, actor, settings
	return nil
}
