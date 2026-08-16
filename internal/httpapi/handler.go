package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/subscription"
)

const (
	sessionCookieName = "vpn_admin_session"
	csrfCookieName    = "vpn_csrf_token"
	csrfHeaderName    = "X-CSRF-Token"
)

type ReadinessProbe interface {
	Ping(context.Context) error
}

type LoginExchanger interface {
	Exchange(ctx context.Context, telegramID int64, code string) (sessionToken string, csrfToken string, err error)
}

type SessionManager interface {
	Authenticate(ctx context.Context, token string) (auth.Administrator, error)
	Revoke(ctx context.Context, token string) error
}

type SubscriptionRenderer interface {
	Render(context.Context, string, subscription.Format) ([]byte, error)
}

type LoginProtection struct {
	SourceIPs SourceIPResolver
	Limiter   *LoginRateLimiter
}

func NewHandler(readiness ReadinessProbe, loginExchangers ...LoginExchanger) http.Handler {
	var login LoginExchanger
	if len(loginExchangers) > 0 {
		login = loginExchangers[0]
	}
	return newHandler(readiness, login, nil, nil)
}

func NewHandlerWithSubscription(readiness ReadinessProbe, subscriptions SubscriptionRenderer) http.Handler {
	return newHandler(readiness, nil, nil, nil, subscriptions)
}

func NewAuthenticatedHandler(readiness ReadinessProbe, login LoginExchanger, sessions SessionManager, protections ...LoginProtection) http.Handler {
	var protection *LoginProtection
	if len(protections) > 0 {
		protection = &protections[0]
	}
	return newHandler(readiness, login, sessions, protection, nil)
}

func NewAuthenticatedHandlerWithSubscription(readiness ReadinessProbe, login LoginExchanger, sessions SessionManager, protection LoginProtection, subscriptions SubscriptionRenderer) http.Handler {
	return newHandler(readiness, login, sessions, &protection, subscriptions)
}

func NewApplicationHandler(readiness ReadinessProbe, login LoginExchanger, sessions SessionManager, protection LoginProtection, subscriptions SubscriptionRenderer, users UserManager, provisioning UserProvisioningManager, managementOptions ...any) http.Handler {
	options := []any{&protection, subscriptions, users, provisioning}
	options = append(options, managementOptions...)
	return newHandler(readiness, login, sessions, options...)
}

func newHandler(readiness ReadinessProbe, login LoginExchanger, sessions SessionManager, protections ...any) http.Handler {
	var protection *LoginProtection
	var subscriptions SubscriptionRenderer
	var users UserManager
	var provisioning UserProvisioningManager
	var administrators AdministratorManager
	var audits AuditReader
	var managementSettings ManagementSettingsManager
	for _, option := range protections {
		switch value := option.(type) {
		case *LoginProtection:
			protection = value
		case SubscriptionRenderer:
			subscriptions = value
		case UserManager:
			users = value
		case UserProvisioningManager:
			provisioning = value
		case AdministratorManager:
			administrators = value
		case AuditReader:
			audits = value
		case ManagementSettingsManager:
			managementSettings = value
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(response http.ResponseWriter, _ *http.Request) {
		writeHealth(response, http.StatusOK, "ok")
	})
	mux.HandleFunc("GET /health/ready", func(response http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if readiness.Ping(ctx) != nil {
			writeHealth(response, http.StatusServiceUnavailable, "unavailable")
			return
		}
		writeHealth(response, http.StatusOK, "ready")
	})
	if login != nil {
		mux.HandleFunc("POST /api/auth/login", adminLoginHandler(login, protection))
	}
	if sessions != nil {
		mux.HandleFunc("GET /api/auth/me", adminIdentityHandler(sessions))
		mux.HandleFunc("POST /api/auth/logout", adminLogoutHandler(sessions))
	}
	if subscriptions != nil {
		mux.HandleFunc("GET /sub/{token}", subscriptionHandler(subscriptions))
	}
	if sessions != nil && users != nil && provisioning != nil {
		registerManagementRoutes(mux, sessions, users, provisioning)
	}
	if sessions != nil && administrators != nil {
		registerAdministratorRoutes(mux, sessions, administrators)
	}
	if sessions != nil && audits != nil {
		registerAuditRoutes(mux, sessions, audits)
	}
	if sessions != nil && managementSettings != nil {
		registerManagementSettingsRoutes(mux, sessions, managementSettings)
	}
	return mux
}

func subscriptionHandler(renderer SubscriptionRenderer) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		format, err := subscription.NegotiateFormat(request.URL.Query().Get("format"), request.UserAgent())
		if err != nil {
			writeError(response, http.StatusBadRequest, "subscription_format_invalid")
			return
		}
		body, err := renderer.Render(request.Context(), request.PathValue("token"), format)
		if err != nil {
			writeError(response, http.StatusNotFound, "subscription_not_found")
			return
		}
		contentType := "text/plain; charset=utf-8"
		if format == subscription.FormatSingBox || format == subscription.FormatClash {
			contentType = "application/json"
		}
		response.Header().Set("Content-Type", contentType)
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(body)
	}
}

func adminLoginHandler(exchanger LoginExchanger, protection *LoginProtection) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeError(response, http.StatusUnsupportedMediaType, "content_type_invalid")
			return
		}
		var input struct {
			TelegramID int64  `json:"telegram_id"`
			Code       string `json:"code"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || input.TelegramID <= 0 || input.Code == "" {
			writeError(response, http.StatusBadRequest, "request_invalid")
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeError(response, http.StatusBadRequest, "request_invalid")
			return
		}
		var attempt *LoginAttempt
		if protection != nil && protection.Limiter != nil {
			var allowed bool
			attempt, allowed = protection.Limiter.Begin(protection.SourceIPs.Resolve(request), input.TelegramID)
			if !allowed {
				response.Header().Set("Retry-After", "900")
				writeError(response, http.StatusTooManyRequests, "login_rate_limited")
				return
			}
		}
		sessionToken, csrfToken, err := exchanger.Exchange(request.Context(), input.TelegramID, input.Code)
		if err != nil {
			attempt.Complete(false)
			writeError(response, http.StatusUnauthorized, "login_invalid")
			return
		}
		attempt.Complete(true)
		setAuthCookie(response, sessionCookieName, sessionToken, true)
		setAuthCookie(response, csrfCookieName, csrfToken, false)
		writeJSON(response, http.StatusOK, struct {
			CSRFToken string `json:"csrf_token"`
		}{CSRFToken: csrfToken})
	}
}

func adminIdentityHandler(sessions SessionManager) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		_, administrator, ok := authenticateRequest(response, request, sessions)
		if !ok {
			return
		}
		writeJSON(response, http.StatusOK, struct {
			TelegramID int64     `json:"telegram_id"`
			Role       auth.Role `json:"role"`
			Root       bool      `json:"root"`
		}{
			TelegramID: administrator.TelegramID,
			Role:       administrator.Role,
			Root:       administrator.Root,
		})
	}
}

func adminLogoutHandler(sessions SessionManager) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		token, _, ok := authenticateRequest(response, request, sessions)
		if !ok {
			return
		}
		if !validCSRF(request) {
			writeError(response, http.StatusForbidden, "csrf_invalid")
			return
		}
		if err := sessions.Revoke(request.Context(), token); err != nil {
			writeError(response, http.StatusInternalServerError, "session_revoke_failed")
			return
		}
		clearAuthCookie(response, sessionCookieName, true)
		clearAuthCookie(response, csrfCookieName, false)
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusNoContent)
	}
}

func authenticateRequest(response http.ResponseWriter, request *http.Request, sessions SessionManager) (string, auth.Administrator, bool) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		writeError(response, http.StatusUnauthorized, "session_invalid")
		return "", auth.Administrator{}, false
	}
	administrator, err := sessions.Authenticate(request.Context(), cookie.Value)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "session_invalid")
		return "", auth.Administrator{}, false
	}
	return cookie.Value, administrator, true
}

func validCSRF(request *http.Request) bool {
	cookie, err := request.Cookie(csrfCookieName)
	if err != nil {
		return false
	}
	cookieToken, cookieOK := decodeCSRF(cookie.Value)
	headerToken, headerOK := decodeCSRF(request.Header.Get(csrfHeaderName))
	return cookieOK && headerOK && subtle.ConstantTimeCompare(cookieToken, headerToken) == 1
}

func decodeCSRF(token string) ([]byte, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return decoded, err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == token
}

func setAuthCookie(response http.ResponseWriter, name, value string, httpOnly bool) {
	http.SetCookie(response, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   7 * 24 * 60 * 60,
		Secure:   true,
		HttpOnly: httpOnly,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearAuthCookie(response http.ResponseWriter, name string, httpOnly bool) {
	http.SetCookie(response, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0).UTC(),
		Secure:   true,
		HttpOnly: httpOnly,
		SameSite: http.SameSiteStrictMode,
	})
}

func writeHealth(response http.ResponseWriter, statusCode int, status string) {
	writeJSON(response, statusCode, struct {
		Status string `json:"status"`
	}{Status: status})
}

func writeError(response http.ResponseWriter, statusCode int, code string) {
	writeJSON(response, statusCode, struct {
		Error string `json:"error"`
	}{Error: code})
}

func writeJSON(response http.ResponseWriter, statusCode int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(statusCode)
	_ = json.NewEncoder(response).Encode(value)
}
