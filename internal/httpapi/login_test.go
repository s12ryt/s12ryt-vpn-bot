package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdminLoginExchangesCodeForSecureSessionAndCSRFToken(t *testing.T) {
	exchanger := &loginExchangerStub{sessionToken: "session-secret", csrfToken: "csrf-secret"}
	handler := NewHandler(readinessStub{}, exchanger)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"telegram_id":12345,"code":"Ab12Cd34"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", response.Code, response.Body.String())
	}
	if exchanger.code != "Ab12Cd34" {
		t.Fatalf("exchanged code = %q, want Ab12Cd34", exchanger.code)
	}
	if exchanger.telegramID != 12345 {
		t.Fatalf("exchanged Telegram ID = %d, want 12345", exchanger.telegramID)
	}
	if !strings.Contains(response.Body.String(), `"csrf_token":"csrf-secret"`) {
		t.Fatalf("body = %q, want CSRF token", response.Body.String())
	}
	assertNoStoreJSON(t, response)
	cookies := response.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("cookies = %d, want session and CSRF cookies", len(cookies))
	}
	assertCookie(t, cookies, "vpn_admin_session", "session-secret", true)
	assertCookie(t, cookies, "vpn_csrf_token", "csrf-secret", false)
}

func TestAdminLoginRejectsInvalidRequestsWithoutLeakingDetails(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		exchangeErr error
		wantStatus  int
		wantCalls   int
	}{
		{name: "content type", contentType: "text/plain", body: `{"telegram_id":12345,"code":"Ab12Cd34"}`, wantStatus: http.StatusUnsupportedMediaType},
		{name: "malformed JSON", contentType: "application/json", body: `{`, wantStatus: http.StatusBadRequest},
		{name: "unknown field", contentType: "application/json", body: `{"telegram_id":12345,"code":"Ab12Cd34","admin":true}`, wantStatus: http.StatusBadRequest},
		{name: "missing Telegram ID", contentType: "application/json", body: `{"code":"Ab12Cd34"}`, wantStatus: http.StatusBadRequest},
		{name: "invalid Telegram ID", contentType: "application/json", body: `{"telegram_id":0,"code":"Ab12Cd34"}`, wantStatus: http.StatusBadRequest},
		{name: "empty code", contentType: "application/json", body: `{"telegram_id":12345,"code":""}`, wantStatus: http.StatusBadRequest},
		{name: "invalid code", contentType: "application/json", body: `{"telegram_id":12345,"code":"Ab12Cd34"}`, exchangeErr: errors.New("database detail: code expired"), wantStatus: http.StatusUnauthorized, wantCalls: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exchanger := &loginExchangerStub{err: test.exchangeErr}
			handler := NewHandler(readinessStub{}, exchanger)
			request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %q", response.Code, test.wantStatus, response.Body.String())
			}
			if exchanger.calls != test.wantCalls {
				t.Fatalf("exchange calls = %d, want %d", exchanger.calls, test.wantCalls)
			}
			if strings.Contains(response.Body.String(), "database detail") {
				t.Fatalf("response leaked internal error: %q", response.Body.String())
			}
			assertNoStoreJSON(t, response)
		})
	}
}

func TestAdminLoginAppliesSourceIPAndAccountRateLimit(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	limiter, err := NewLoginRateLimiter(LoginRateLimits{
		AccountAttempts: 1, AccountWindow: 15 * time.Minute,
		IPAttempts: 20, IPWindow: 15 * time.Minute,
		GlobalAttempts: 100, GlobalWindow: time.Minute,
	}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewLoginRateLimiter() error = %v", err)
	}
	exchanger := &loginExchangerStub{err: errors.New("invalid")}
	handler := NewAuthenticatedHandler(readinessStub{}, exchanger, nil, LoginProtection{
		SourceIPs: NewSourceIPResolver(nil),
		Limiter:   limiter,
	})

	for attempt, wantStatus := range []int{http.StatusUnauthorized, http.StatusTooManyRequests} {
		request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"telegram_id":12345,"code":"Ab12Cd34"}`))
		request.RemoteAddr = "198.51.100.10:4321"
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)

		if response.Code != wantStatus {
			t.Fatalf("attempt %d status = %d, want %d", attempt+1, response.Code, wantStatus)
		}
		if wantStatus == http.StatusTooManyRequests && response.Header().Get("Retry-After") != "900" {
			t.Fatalf("Retry-After = %q, want opaque fixed 900", response.Header().Get("Retry-After"))
		}
	}
	if exchanger.calls != 1 {
		t.Fatalf("Exchange() calls = %d, want rate-limited attempt blocked before exchange", exchanger.calls)
	}
}

type loginExchangerStub struct {
	code         string
	telegramID   int64
	sessionToken string
	csrfToken    string
	err          error
	calls        int
}

func (stub *loginExchangerStub) Exchange(_ context.Context, telegramID int64, code string) (string, string, error) {
	stub.calls++
	stub.telegramID = telegramID
	stub.code = code
	return stub.sessionToken, stub.csrfToken, stub.err
}

func assertCookie(t *testing.T, cookies []*http.Cookie, name, value string, httpOnly bool) {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name != name {
			continue
		}
		if cookie.Value != value || cookie.Path != "/" || !cookie.Secure || cookie.HttpOnly != httpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge != 7*24*60*60 {
			t.Fatalf("cookie %s has insecure attributes: %#v", name, cookie)
		}
		return
	}
	t.Fatalf("cookie %s not found", name)
}

func assertNoStoreJSON(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}
