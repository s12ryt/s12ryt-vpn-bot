package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type readinessStub struct {
	err error
}

func (stub readinessStub) Ping(context.Context) error {
	return stub.err
}

func TestLiveEndpointReportsProcessHealth(t *testing.T) {
	handler := NewHandler(readinessStub{err: errors.New("database unavailable")})
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if !strings.Contains(response.Body.String(), `"status":"ok"`) {
		t.Fatalf("body = %q, want live status", response.Body.String())
	}
	assertHealthHeaders(t, response)
}

func TestReadyEndpointReflectsDatabaseAvailability(t *testing.T) {
	tests := []struct {
		name       string
		pingErr    error
		wantStatus int
		wantBody   string
	}{
		{name: "ready", wantStatus: http.StatusOK, wantBody: `"status":"ready"`},
		{
			name:       "database unavailable",
			pingErr:    errors.New("connection refused: secret details"),
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `"status":"unavailable"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewHandler(readinessStub{err: test.pingErr})
			request := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			body := response.Body.String()
			if !strings.Contains(body, test.wantBody) {
				t.Fatalf("body = %q, want %s", body, test.wantBody)
			}
			if strings.Contains(body, "secret details") {
				t.Fatalf("readiness response leaked dependency error: %q", body)
			}
			assertHealthHeaders(t, response)
		})
	}
}

func assertHealthHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}
