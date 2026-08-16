package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

type CoreSettingsManager interface {
	GetCore(context.Context) (domain.CoreSettingsOverview, error)
	UpdateCore(context.Context, int64, domain.CoreSettingsUpdate, time.Time) error
}

func registerCoreSettingsRoutes(mux *http.ServeMux, sessions SessionManager, manager CoreSettingsManager) {
	mux.HandleFunc("GET /api/settings/core", func(response http.ResponseWriter, request *http.Request) {
		_, _, ok := authorize(response, request, sessions, auth.PermissionManageVPNSettings, false)
		if !ok {
			return
		}
		settings, err := manager.GetCore(request.Context())
		if err != nil {
			writeError(response, http.StatusInternalServerError, "core_settings_unavailable")
			return
		}
		writeJSON(response, http.StatusOK, settings)
	})

	mux.HandleFunc("PUT /api/settings/core", func(response http.ResponseWriter, request *http.Request) {
		_, administrator, ok := authorize(response, request, sessions, auth.PermissionManageVPNSettings, true)
		if !ok {
			return
		}
		var input struct {
			Configured         bool   `json:"configured"`
			ListenIPv4         string `json:"listen_ipv4"`
			ListenIPv6         string `json:"listen_ipv6"`
			VLESSPort          uint16 `json:"vless_port"`
			Hysteria2Port      uint16 `json:"hysteria2_port"`
			TUICPort           uint16 `json:"tuic_port"`
			AnyTLSPort         uint16 `json:"anytls_port"`
			TLSServerName      string `json:"tls_server_name"`
			TLSCertificatePath string `json:"tls_certificate_path"`
			TLSKeyPath         string `json:"tls_key_path"`
			RealityServer      string `json:"reality_server"`
			RealityServerPort  uint16 `json:"reality_server_port"`
			RealityPrivateKey  string `json:"reality_private_key"`
			RealityShortID     string `json:"reality_short_id"`
			StatsListen        string `json:"stats_listen"`
			AllowIPv4Outbound  bool   `json:"allow_ipv4_outbound"`
		}
		if !decodeStrictJSON(response, request, &input) {
			return
		}
		update := domain.CoreSettingsUpdate{
			CoreSettingsOverview: domain.CoreSettingsOverview{
				Configured: input.Configured, ListenIPv4: input.ListenIPv4, ListenIPv6: input.ListenIPv6,
				VLESSPort: input.VLESSPort, Hysteria2Port: input.Hysteria2Port, TUICPort: input.TUICPort, AnyTLSPort: input.AnyTLSPort,
				TLSServerName: input.TLSServerName, TLSCertificatePath: input.TLSCertificatePath, TLSKeyPath: input.TLSKeyPath,
				RealityServer: input.RealityServer, RealityServerPort: input.RealityServerPort,
				RealityShortID: input.RealityShortID, StatsListen: input.StatsListen, AllowIPv4Outbound: input.AllowIPv4Outbound,
			},
			RealityPrivateKey: input.RealityPrivateKey,
		}
		if err := manager.UpdateCore(request.Context(), administrator.TelegramID, update, time.Now().UTC()); err != nil {
			writeError(response, http.StatusConflict, "core_settings_operation_failed")
			return
		}
		writeNoContent(response)
	})
}
