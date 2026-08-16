package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
)

type AdministratorManager interface {
	List(context.Context) ([]auth.Administrator, error)
	SetRole(context.Context, int64, int64, auth.Role, time.Time) error
	Remove(context.Context, int64, int64, time.Time) error
}

func registerAdministratorRoutes(mux *http.ServeMux, sessions SessionManager, administrators AdministratorManager) {
	mux.HandleFunc("GET /api/administrators", func(response http.ResponseWriter, request *http.Request) {
		_, _, ok := authorize(response, request, sessions, auth.PermissionManageRoles, false)
		if !ok {
			return
		}
		items, err := administrators.List(request.Context())
		if err != nil {
			writeError(response, http.StatusInternalServerError, "administrators_unavailable")
			return
		}
		type administratorResponse struct {
			TelegramID int64     `json:"telegram_id"`
			Role       auth.Role `json:"role"`
			Root       bool      `json:"root"`
		}
		result := make([]administratorResponse, len(items))
		for index, item := range items {
			result[index] = administratorResponse{TelegramID: item.TelegramID, Role: item.Role, Root: item.Root}
		}
		writeJSON(response, http.StatusOK, struct {
			Administrators []administratorResponse `json:"administrators"`
		}{Administrators: result})
	})

	mux.HandleFunc("POST /api/administrators/{telegram_id}/role", func(response http.ResponseWriter, request *http.Request) {
		actor, target, ok := authorizeMutation(response, request, sessions, auth.PermissionManageRoles)
		if !ok {
			return
		}
		var input struct {
			Role auth.Role `json:"role"`
		}
		if !decodeStrictJSON(response, request, &input) {
			return
		}
		if input.Role != auth.RoleOwner && input.Role != auth.RoleAdministrator {
			writeError(response, http.StatusBadRequest, "request_invalid")
			return
		}
		if err := administrators.SetRole(request.Context(), actor.TelegramID, target, input.Role, time.Now().UTC()); err != nil {
			writeError(response, http.StatusConflict, "administrator_operation_failed")
			return
		}
		writeNoContent(response)
	})

	mux.HandleFunc("DELETE /api/administrators/{telegram_id}", func(response http.ResponseWriter, request *http.Request) {
		actor, target, ok := authorizeMutation(response, request, sessions, auth.PermissionManageRoles)
		if !ok {
			return
		}
		if err := administrators.Remove(request.Context(), actor.TelegramID, target, time.Now().UTC()); err != nil {
			writeError(response, http.StatusConflict, "administrator_operation_failed")
			return
		}
		writeNoContent(response)
	})
}
