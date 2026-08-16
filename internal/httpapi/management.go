package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

type UserManager interface {
	ListUsers(context.Context, int64, int) ([]domain.UserOverview, error)
	Revoke(context.Context, int64, int64, domain.RevocationMode, time.Time) error
	RejectApproval(context.Context, int64, int64, time.Time) error
}

type UserProvisioningManager interface {
	Approve(context.Context, int64, time.Time) (domain.ProvisionedAccess, error)
	Rotate(context.Context, int64, time.Time, bool) (domain.ProvisionedAccess, error)
}

func registerManagementRoutes(mux *http.ServeMux, sessions SessionManager, users UserManager, provisioning UserProvisioningManager) {
	mux.HandleFunc("GET /api/users", func(response http.ResponseWriter, request *http.Request) {
		_, administrator, ok := authorize(response, request, sessions, auth.PermissionManageUsers, false)
		if !ok || administrator.TelegramID <= 0 {
			return
		}
		after, limit, ok := parseListWindow(request)
		if !ok {
			writeError(response, http.StatusBadRequest, "request_invalid")
			return
		}
		items, err := users.ListUsers(request.Context(), after, limit)
		if err != nil {
			writeError(response, http.StatusInternalServerError, "users_unavailable")
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Users []domain.UserOverview `json:"users"`
		}{Users: items})
	})
	mux.HandleFunc("POST /api/users/{telegram_id}/revoke", func(response http.ResponseWriter, request *http.Request) {
		administrator, target, ok := authorizeMutation(response, request, sessions, auth.PermissionManageUsers)
		if !ok {
			return
		}
		var input struct {
			Mode domain.RevocationMode `json:"mode"`
		}
		if !decodeStrictJSON(response, request, &input) {
			return
		}
		if err := users.Revoke(request.Context(), administrator.TelegramID, target, input.Mode, time.Now().UTC()); err != nil {
			writeError(response, http.StatusConflict, "user_operation_failed")
			return
		}
		writeNoContent(response)
	})
	mux.HandleFunc("POST /api/users/{telegram_id}/reject", func(response http.ResponseWriter, request *http.Request) {
		administrator, target, ok := authorizeMutation(response, request, sessions, auth.PermissionManageApprovals)
		if !ok {
			return
		}
		if err := users.RejectApproval(request.Context(), administrator.TelegramID, target, time.Now().UTC()); err != nil {
			writeError(response, http.StatusConflict, "user_operation_failed")
			return
		}
		writeNoContent(response)
	})
	mux.HandleFunc("POST /api/users/{telegram_id}/approve", func(response http.ResponseWriter, request *http.Request) {
		_, target, ok := authorizeMutation(response, request, sessions, auth.PermissionManageApprovals)
		if !ok {
			return
		}
		if _, err := provisioning.Approve(request.Context(), target, time.Now().UTC()); err != nil {
			writeError(response, http.StatusConflict, "user_operation_failed")
			return
		}
		writeNoContent(response)
	})
	mux.HandleFunc("POST /api/users/{telegram_id}/rotate", func(response http.ResponseWriter, request *http.Request) {
		_, target, ok := authorizeMutation(response, request, sessions, auth.PermissionManageUsers)
		if !ok {
			return
		}
		var input struct {
			ResetPeriod bool `json:"reset_period"`
		}
		if !decodeStrictJSON(response, request, &input) {
			return
		}
		if _, err := provisioning.Rotate(request.Context(), target, time.Now().UTC(), input.ResetPeriod); err != nil {
			writeError(response, http.StatusConflict, "user_operation_failed")
			return
		}
		writeNoContent(response)
	})
}

func authorize(response http.ResponseWriter, request *http.Request, sessions SessionManager, permission auth.Permission, csrf bool) (string, auth.Administrator, bool) {
	token, administrator, ok := authenticateRequest(response, request, sessions)
	if !ok {
		return "", auth.Administrator{}, false
	}
	if !administrator.Active || !administrator.Role.Allows(permission) {
		writeError(response, http.StatusForbidden, "permission_denied")
		return "", auth.Administrator{}, false
	}
	if csrf && !validCSRF(request) {
		writeError(response, http.StatusForbidden, "csrf_invalid")
		return "", auth.Administrator{}, false
	}
	return token, administrator, true
}

func authorizeMutation(response http.ResponseWriter, request *http.Request, sessions SessionManager, permission auth.Permission) (auth.Administrator, int64, bool) {
	_, administrator, ok := authorize(response, request, sessions, permission, true)
	if !ok {
		return auth.Administrator{}, 0, false
	}
	target, err := strconv.ParseInt(request.PathValue("telegram_id"), 10, 64)
	if err != nil || target <= 0 || strconv.FormatInt(target, 10) != request.PathValue("telegram_id") {
		writeError(response, http.StatusBadRequest, "request_invalid")
		return auth.Administrator{}, 0, false
	}
	return administrator, target, true
}

func parseListWindow(request *http.Request) (int64, int, bool) {
	after, limit := int64(0), 50
	var err error
	if raw := request.URL.Query().Get("after"); raw != "" {
		after, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || after < 0 {
			return 0, 0, false
		}
	}
	if raw := request.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			return 0, 0, false
		}
	}
	return after, limit, true
}

func decodeStrictJSON(response http.ResponseWriter, request *http.Request, destination any) bool {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(response, http.StatusUnsupportedMediaType, "content_type_invalid")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(response, http.StatusBadRequest, "request_invalid")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "request_invalid")
		return false
	}
	return true
}

func writeNoContent(response http.ResponseWriter) {
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}
