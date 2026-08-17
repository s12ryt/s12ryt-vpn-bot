package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/reality"
)

// RealitySearchManager triggers and observes background REALITY target
// searches. Results are advisory only; owners still confirm the final target
// through the core settings update flow.
type RealitySearchManager interface {
	Start(context.Context) error
	Snapshot() reality.SearchSnapshot
}

func registerRealitySearchRoutes(mux *http.ServeMux, sessions SessionManager, manager RealitySearchManager) {
	mux.HandleFunc("POST /api/settings/reality/search", func(response http.ResponseWriter, request *http.Request) {
		if _, _, ok := authorize(response, request, sessions, auth.PermissionManageVPNSettings, true); !ok {
			return
		}
		// The background search must outlive the triggering HTTP request.
		if err := manager.Start(context.WithoutCancel(request.Context())); err != nil {
			if errors.Is(err, reality.ErrSearchRunning) {
				writeError(response, http.StatusConflict, "reality_search_running")
				return
			}
			writeError(response, http.StatusInternalServerError, "reality_search_start_failed")
			return
		}
		writeJSON(response, http.StatusAccepted, struct {
			Status reality.SearchStatus `json:"status"`
		}{Status: reality.SearchStatusRunning})
	})

	mux.HandleFunc("GET /api/settings/reality/search", func(response http.ResponseWriter, request *http.Request) {
		if _, _, ok := authorize(response, request, sessions, auth.PermissionManageVPNSettings, false); !ok {
			return
		}
		snapshot := manager.Snapshot()
		writeJSON(response, http.StatusOK, snapshot)
	})
}
