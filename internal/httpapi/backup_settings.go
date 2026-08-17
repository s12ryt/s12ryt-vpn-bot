package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

type BackupSettingsManager interface {
	GetBackupSettings(context.Context) (domain.BackupSettings, error)
	UpdateBackupSettings(context.Context, int64, domain.BackupSettings, time.Time) error
}

func registerBackupSettingsRoutes(mux *http.ServeMux, sessions SessionManager, manager BackupSettingsManager) {
	mux.HandleFunc("GET /api/settings/backup", func(response http.ResponseWriter, request *http.Request) {
		if _, _, ok := authorize(response, request, sessions, auth.PermissionManageGlobalSettings, false); !ok {
			return
		}
		settings, err := manager.GetBackupSettings(request.Context())
		if err != nil {
			writeError(response, http.StatusInternalServerError, "backup_settings_unavailable")
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Settings domain.BackupSettings `json:"settings"`
		}{Settings: settings})
	})
	mux.HandleFunc("PUT /api/settings/backup", func(response http.ResponseWriter, request *http.Request) {
		_, administrator, ok := authorize(response, request, sessions, auth.PermissionManageGlobalSettings, true)
		if !ok {
			return
		}
		var settings domain.BackupSettings
		if !decodeStrictJSON(response, request, &settings) {
			return
		}
		if err := settings.Validate(); err != nil {
			writeError(response, http.StatusBadRequest, "request_invalid")
			return
		}
		if err := manager.UpdateBackupSettings(request.Context(), administrator.TelegramID, settings, time.Now().UTC()); err != nil {
			writeError(response, http.StatusConflict, "backup_settings_operation_failed")
			return
		}
		writeNoContent(response)
	})
}
