package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestAdminLoginAuditsSuccessfulAndFailedCredentialExchanges(t *testing.T) {
	now := time.Date(2026, time.August, 17, 14, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name        string
		exchangeErr error
		wantStatus  int
		wantSuccess bool
	}{
		{name: "success", wantStatus: http.StatusOK, wantSuccess: true},
		{name: "failure", exchangeErr: errors.New("secret database detail"), wantStatus: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			auditor := &loginAuditorStub{}
			exchanger := &loginExchangerStub{sessionToken: "session-secret", csrfToken: testCSRFToken, err: test.exchangeErr}
			handler := NewAuthenticatedHandler(readinessStub{}, exchanger, &sessionManagerStub{}, LoginProtection{
				SourceIPs: NewSourceIPResolver(nil),
				Auditor:   auditor,
				Now:       func() time.Time { return now },
			})
			request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"telegram_id":12345,"code":"Ab12Cd34"}`))
			request.RemoteAddr = "198.51.100.8:4321"
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%q", response.Code, test.wantStatus, response.Body.String())
			}
			if auditor.calls != 1 || auditor.telegramID != 12345 || auditor.sourceIP != netip.MustParseAddr("198.51.100.8") || auditor.success != test.wantSuccess || !auditor.at.Equal(now) {
				t.Fatalf("audit = calls:%d telegram:%d source:%v success:%v at:%v", auditor.calls, auditor.telegramID, auditor.sourceIP, auditor.success, auditor.at)
			}
			if strings.Contains(response.Body.String(), "secret database detail") {
				t.Fatalf("response leaked exchange error: %q", response.Body.String())
			}
		})
	}
}

func TestAdminLoginRevokesNewSessionWhenSuccessAuditCannotBePersisted(t *testing.T) {
	sessions := &sessionManagerStub{}
	auditor := &loginAuditorStub{err: errors.New("audit database unavailable")}
	handler := NewAuthenticatedHandler(readinessStub{}, &loginExchangerStub{sessionToken: "new-session", csrfToken: testCSRFToken}, sessions, LoginProtection{
		SourceIPs: NewSourceIPResolver(nil),
		Auditor:   auditor,
		Now:       time.Now,
	})
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"telegram_id":12345,"code":"Ab12Cd34"}`))
	request.RemoteAddr = "198.51.100.8:4321"
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"login_audit_failed"`) {
		t.Fatalf("response = %d %q, want opaque audit failure", response.Code, response.Body.String())
	}
	if sessions.revokedToken != "new-session" {
		t.Fatalf("revoked token = %q, want new-session", sessions.revokedToken)
	}
	if cookies := response.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("audit failure issued cookies: %#v", cookies)
	}
}

func TestAdminLoginAuditFailureDoesNotRevealCredentialValidity(t *testing.T) {
	auditor := &loginAuditorStub{err: errors.New("audit database unavailable")}
	handler := NewAuthenticatedHandler(readinessStub{}, &loginExchangerStub{err: errors.New("invalid code")}, &sessionManagerStub{}, LoginProtection{
		SourceIPs: NewSourceIPResolver(nil),
		Auditor:   auditor,
		Now:       time.Now,
	})
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"telegram_id":12345,"code":"Ab12Cd34"}`))
	request.RemoteAddr = "198.51.100.8:4321"
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable || response.Body.String() != "{\"error\":\"login_audit_failed\"}\n" {
		t.Fatalf("response = %d %q, want identical opaque audit failure", response.Code, response.Body.String())
	}
}

type loginAuditorStub struct {
	telegramID int64
	sourceIP   netip.Addr
	success    bool
	at         time.Time
	err        error
	calls      int
}

func (stub *loginAuditorStub) RecordLoginAttempt(_ context.Context, telegramID int64, sourceIP netip.Addr, success bool, at time.Time) error {
	stub.calls++
	stub.telegramID = telegramID
	stub.sourceIP = sourceIP
	stub.success = success
	stub.at = at
	return stub.err
}
