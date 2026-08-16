package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

type ManagementSettingsManager interface {
	Get(context.Context) (domain.ManagementSettings, []domain.QualificationRuleOverview, error)
	PreviewInactivity(context.Context, int, time.Time) (int64, error)
	Update(context.Context, int64, domain.ManagementSettings, bool, time.Time) error
}

func registerManagementSettingsRoutes(mux *http.ServeMux, sessions SessionManager, manager ManagementSettingsManager) {
	mux.HandleFunc("GET /api/settings/management", func(response http.ResponseWriter, request *http.Request) {
		_, _, ok := authorize(response, request, sessions, auth.PermissionManageGlobalSettings, false)
		if !ok {
			return
		}
		settings, rules, err := manager.Get(request.Context())
		if err != nil {
			writeError(response, http.StatusInternalServerError, "settings_unavailable")
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Settings domain.ManagementSettings          `json:"settings"`
			Rules    []domain.QualificationRuleOverview `json:"rules"`
		}{Settings: settings, Rules: rules})
	})
	mux.HandleFunc("POST /api/settings/management/inactivity-preview", func(response http.ResponseWriter, request *http.Request) {
		_, _, ok := authorize(response, request, sessions, auth.PermissionManageGlobalSettings, true)
		if !ok {
			return
		}
		var input struct {
			ThresholdDays int `json:"threshold_days"`
		}
		if !decodeStrictJSON(response, request, &input) {
			return
		}
		if input.ThresholdDays < 1 {
			writeError(response, http.StatusBadRequest, "request_invalid")
			return
		}
		count, err := manager.PreviewInactivity(request.Context(), input.ThresholdDays, time.Now().UTC())
		if err != nil {
			writeError(response, http.StatusInternalServerError, "settings_unavailable")
			return
		}
		writeJSON(response, http.StatusOK, struct {
			AffectedUsers int64 `json:"affected_users"`
		}{AffectedUsers: count})
	})
	mux.HandleFunc("PUT /api/settings/management", func(response http.ResponseWriter, request *http.Request) {
		_, administrator, ok := authorize(response, request, sessions, auth.PermissionManageGlobalSettings, true)
		if !ok {
			return
		}
		var input struct {
			domain.ManagementSettings
			ConfirmInactivityRemoval bool `json:"confirm_inactivity_removal"`
		}
		if !decodeStrictJSON(response, request, &input) {
			return
		}
		if err := input.ManagementSettings.Validate(); err != nil {
			writeError(response, http.StatusBadRequest, "request_invalid")
			return
		}
		if err := manager.Update(request.Context(), administrator.TelegramID, input.ManagementSettings, input.ConfirmInactivityRemoval, time.Now().UTC()); err != nil {
			writeError(response, http.StatusConflict, "settings_operation_failed")
			return
		}
		writeNoContent(response)
	})
}
