package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

// TLSSettingsManager persists the owner-managed ACME/TLS configuration.
type TLSSettingsManager interface {
	GetOverview(context.Context) (domain.TLSSettingsOverview, error)
	Save(context.Context, int64, domain.TLSSettingsUpdate, time.Time) error
}

func registerTLSSettingsRoutes(mux *http.ServeMux, sessions SessionManager, manager TLSSettingsManager) {
	if manager == nil {
		return
	}
	mux.HandleFunc("GET /api/settings/tls", func(response http.ResponseWriter, request *http.Request) {
		_, _, ok := authorize(response, request, sessions, auth.PermissionManageVPNSettings, false)
		if !ok {
			return
		}
		overview, err := manager.GetOverview(request.Context())
		if err != nil {
			writeError(response, http.StatusInternalServerError, "tls_settings_unavailable")
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Overview domain.TLSSettingsOverview `json:"settings"`
		}{Overview: overview})
	})
	mux.HandleFunc("PUT /api/settings/tls", func(response http.ResponseWriter, request *http.Request) {
		_, administrator, ok := authorize(response, request, sessions, auth.PermissionManageVPNSettings, true)
		if !ok {
			return
		}
		var input struct {
			Mode            string   `json:"mode"`
			Domain          string   `json:"domain"`
			Challenge       string   `json:"challenge"`
			Email           string   `json:"email"`
			CADirectoryURLs []string `json:"ca_directory_urls"`
			TermsAccepted   bool     `json:"terms_accepted"`
			DuckDNSToken    string   `json:"duckdns_token"`
		}
		if !decodeStrictJSON(response, request, &input) {
			return
		}
		update := domain.TLSSettingsUpdate{
			Mode: input.Mode, Domain: input.Domain, Challenge: input.Challenge,
			Email: input.Email, CADirectoryURLs: input.CADirectoryURLs,
			TermsAccepted: input.TermsAccepted, DuckDNSToken: input.DuckDNSToken,
		}
		if err := manager.Save(request.Context(), administrator.TelegramID, update, time.Now().UTC()); err != nil {
			writeError(response, http.StatusConflict, "tls_settings_operation_failed")
			return
		}
		writeNoContent(response)
	})
}
