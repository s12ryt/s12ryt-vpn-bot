package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/qualification"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
)

type QualificationRuleManager interface {
	EnableByActor(context.Context, int64, qualification.ManagedRule) error
	DisableByActor(context.Context, int64, int64) error
}

func registerQualificationRuleRoutes(mux *http.ServeMux, sessions SessionManager, manager QualificationRuleManager) {
	mux.HandleFunc("PUT /api/settings/qualification-rules/{chat_id}", func(response http.ResponseWriter, request *http.Request) {
		_, administrator, ok := authorize(response, request, sessions, auth.PermissionManageGlobalSettings, true)
		if !ok {
			return
		}
		chatID, ok := parseQualificationChatID(response, request.PathValue("chat_id"))
		if !ok {
			return
		}
		var input struct {
			ChatType telegram.ChatType `json:"chat_type"`
			Title    string            `json:"title"`
		}
		if !decodeStrictJSON(response, request, &input) {
			return
		}
		if input.ChatType != telegram.ChatSupergroup && input.ChatType != telegram.ChatChannel {
			writeError(response, http.StatusBadRequest, "request_invalid")
			return
		}
		if err := manager.EnableByActor(request.Context(), administrator.TelegramID, qualification.ManagedRule{ChatID: chatID, ChatType: input.ChatType, Title: input.Title}); err != nil {
			writeError(response, http.StatusConflict, "qualification_rule_operation_failed")
			return
		}
		writeNoContent(response)
	})
	mux.HandleFunc("DELETE /api/settings/qualification-rules/{chat_id}", func(response http.ResponseWriter, request *http.Request) {
		_, administrator, ok := authorize(response, request, sessions, auth.PermissionManageGlobalSettings, true)
		if !ok {
			return
		}
		chatID, ok := parseQualificationChatID(response, request.PathValue("chat_id"))
		if !ok {
			return
		}
		if err := manager.DisableByActor(request.Context(), administrator.TelegramID, chatID); err != nil {
			writeError(response, http.StatusConflict, "qualification_rule_operation_failed")
			return
		}
		writeNoContent(response)
	})
}

func parseQualificationChatID(response http.ResponseWriter, raw string) (int64, bool) {
	chatID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || chatID == 0 || strconv.FormatInt(chatID, 10) != raw {
		writeError(response, http.StatusBadRequest, "request_invalid")
		return 0, false
	}
	return chatID, true
}
