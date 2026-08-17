package httpapi

import (
	"context"
	"net/http"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

// DashboardProvider aggregates the operational status shown on the overview.
type DashboardProvider interface {
	Statistics(context.Context) (domain.UserStatistics, error)
	TLSIssued(context.Context) (bool, error)
	CoreConfigured(context.Context) (bool, error)
}

func registerDashboardRoutes(mux *http.ServeMux, sessions SessionManager, provider DashboardProvider) {
	if provider == nil {
		return
	}
	mux.HandleFunc("GET /api/overview", func(response http.ResponseWriter, request *http.Request) {
		_, _, ok := authorize(response, request, sessions, auth.PermissionManageUsers, false)
		if !ok {
			return
		}
		statistics, err := provider.Statistics(request.Context())
		if err != nil {
			writeError(response, http.StatusInternalServerError, "overview_unavailable")
			return
		}
		tlsIssued, err := provider.TLSIssued(request.Context())
		if err != nil {
			writeError(response, http.StatusInternalServerError, "overview_unavailable")
			return
		}
		coreConfigured, err := provider.CoreConfigured(request.Context())
		if err != nil {
			writeError(response, http.StatusInternalServerError, "overview_unavailable")
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Users          domain.UserStatistics `json:"users"`
			TLSIssued      bool                  `json:"tls_issued"`
			CoreConfigured bool                  `json:"core_configured"`
		}{Users: statistics, TLSIssued: tlsIssued, CoreConfigured: coreConfigured})
	})
}
