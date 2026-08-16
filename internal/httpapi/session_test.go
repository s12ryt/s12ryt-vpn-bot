package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
)

const testCSRFToken = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestAdminSessionReturnsCurrentAdministrator(t *testing.T) {
	sessions := &sessionManagerStub{administrator: auth.Administrator{TelegramID: 12345, Role: auth.RoleOwner, Root: true, Active: true}}
	handler := NewAuthenticatedHandler(readinessStub{}, &loginExchangerStub{}, sessions)
	request := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", response.Code, response.Body.String())
	}
	if sessions.authenticatedToken != "session-secret" {
		t.Fatalf("authenticated token = %q", sessions.authenticatedToken)
	}
	if body := response.Body.String(); !strings.Contains(body, `"telegram_id":12345`) || !strings.Contains(body, `"role":"owner"`) || !strings.Contains(body, `"root":true`) {
		t.Fatalf("body = %q, want administrator identity", body)
	}
	assertNoStoreJSON(t, response)
}

func TestAdminSessionRejectsMissingOrInvalidSessionWithoutLeakingDetails(t *testing.T) {
	sessions := &sessionManagerStub{authenticateErr: errors.New("database detail")}
	handler := NewAuthenticatedHandler(readinessStub{}, &loginExchangerStub{}, sessions)

	for _, withCookie := range []bool{false, true} {
		request := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		if withCookie {
			request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "invalid-session"})
		}
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)

		if response.Code != http.StatusUnauthorized || strings.Contains(response.Body.String(), "database detail") {
			t.Fatalf("response = %d %q, want opaque 401", response.Code, response.Body.String())
		}
	}
}

func TestAdminLogoutRequiresMatchingDoubleSubmitCSRF(t *testing.T) {
	tests := []struct {
		name        string
		cookieToken string
		headerToken string
		wantStatus  int
		wantRevoked bool
	}{
		{name: "missing token", wantStatus: http.StatusForbidden},
		{name: "mismatch", cookieToken: testCSRFToken, headerToken: testCSRFToken + "x", wantStatus: http.StatusForbidden},
		{name: "matching", cookieToken: testCSRFToken, headerToken: testCSRFToken, wantStatus: http.StatusNoContent, wantRevoked: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sessions := &sessionManagerStub{administrator: auth.Administrator{TelegramID: 12345, Role: auth.RoleOwner, Active: true}}
			handler := NewAuthenticatedHandler(readinessStub{}, &loginExchangerStub{}, sessions)
			request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
			request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-secret"})
			if test.cookieToken != "" {
				request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: test.cookieToken})
			}
			request.Header.Set(csrfHeaderName, test.headerToken)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %q", response.Code, test.wantStatus, response.Body.String())
			}
			if (sessions.revokedToken != "") != test.wantRevoked {
				t.Fatalf("revoked token = %q, want revoked %v", sessions.revokedToken, test.wantRevoked)
			}
			if test.wantRevoked {
				assertClearedCookie(t, response.Result().Cookies(), sessionCookieName, true)
				assertClearedCookie(t, response.Result().Cookies(), csrfCookieName, false)
			}
		})
	}
}

type sessionManagerStub struct {
	administrator      auth.Administrator
	authenticateErr    error
	revokeErr          error
	authenticatedToken string
	revokedToken       string
}

func (stub *sessionManagerStub) Authenticate(_ context.Context, token string) (auth.Administrator, error) {
	stub.authenticatedToken = token
	return stub.administrator, stub.authenticateErr
}

func (stub *sessionManagerStub) Revoke(_ context.Context, token string) error {
	stub.revokedToken = token
	return stub.revokeErr
}

func assertClearedCookie(t *testing.T, cookies []*http.Cookie, name string, httpOnly bool) {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			if cookie.Value != "" || cookie.MaxAge >= 0 || !cookie.Secure || cookie.HttpOnly != httpOnly || cookie.SameSite != http.SameSiteStrictMode {
				t.Fatalf("cookie %s was not securely cleared: %#v", name, cookie)
			}
			return
		}
	}
	t.Fatalf("cleared cookie %s not found", name)
}
