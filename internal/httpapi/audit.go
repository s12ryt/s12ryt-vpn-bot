package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

type AuditReader interface {
	List(context.Context, int64, int) ([]domain.AuditEvent, error)
}

func registerAuditRoutes(mux *http.ServeMux, sessions SessionManager, audits AuditReader) {
	mux.HandleFunc("GET /api/audit", func(response http.ResponseWriter, request *http.Request) {
		_, _, ok := authorize(response, request, sessions, auth.PermissionViewAudit, false)
		if !ok {
			return
		}
		before, limit, ok := parseAuditWindow(request)
		if !ok {
			writeError(response, http.StatusBadRequest, "request_invalid")
			return
		}
		events, err := audits.List(request.Context(), before, limit)
		if err != nil {
			writeError(response, http.StatusInternalServerError, "audit_unavailable")
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Events []domain.AuditEvent `json:"events"`
		}{Events: events})
	})
}

func parseAuditWindow(request *http.Request) (int64, int, bool) {
	before, limit := int64(0), 50
	var err error
	if raw := request.URL.Query().Get("before"); raw != "" {
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before < 0 || strconv.FormatInt(before, 10) != raw {
			return 0, 0, false
		}
	}
	if raw := request.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 || strconv.Itoa(limit) != raw {
			return 0, 0, false
		}
	}
	return before, limit, true
}
