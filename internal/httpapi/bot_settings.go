package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
)

// BotSettingsOverview exposes only non-secret bot settings to owners.
type BotSettingsOverview struct {
	BotUsername string    `json:"bot_username"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// BotSettingsManager rotates the Telegram bot token. Rotation must verify the
// candidate controls the same bot before anything is persisted or swapped.
type BotSettingsManager interface {
	GetOverview(context.Context) (BotSettingsOverview, error)
	Rotate(context.Context, int64, string, time.Time) error
}

func registerBotSettingsRoutes(mux *http.ServeMux, sessions SessionManager, manager BotSettingsManager) {
	if manager == nil {
		return
	}
	mux.HandleFunc("GET /api/settings/bot", func(response http.ResponseWriter, request *http.Request) {
		_, _, ok := authorize(response, request, sessions, auth.PermissionManageSecrets, false)
		if !ok {
			return
		}
		overview, err := manager.GetOverview(request.Context())
		if err != nil {
			writeError(response, http.StatusInternalServerError, "bot_settings_unavailable")
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Overview BotSettingsOverview `json:"settings"`
		}{Overview: overview})
	})
	mux.HandleFunc("PUT /api/settings/bot", func(response http.ResponseWriter, request *http.Request) {
		_, administrator, ok := authorize(response, request, sessions, auth.PermissionManageSecrets, true)
		if !ok {
			return
		}
		var input struct {
			BotToken string `json:"bot_token"`
		}
		if !decodeStrictJSON(response, request, &input) {
			return
		}
		if input.BotToken == "" {
			writeError(response, http.StatusBadRequest, "request_invalid")
			return
		}
		if err := manager.Rotate(request.Context(), administrator.TelegramID, input.BotToken, time.Now().UTC()); err != nil {
			switch {
			case errors.Is(err, telegram.ErrBotIdentityChanged):
				writeError(response, http.StatusConflict, "bot_identity_changed")
			case errors.Is(err, telegram.ErrBotVerificationFailed):
				writeError(response, http.StatusConflict, "bot_verification_failed")
			default:
				writeError(response, http.StatusConflict, "bot_settings_operation_failed")
			}
			return
		}
		writeNoContent(response)
	})
}
