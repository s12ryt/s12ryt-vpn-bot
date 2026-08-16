package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/subscription"
)

type subscriptionStub struct {
	format subscription.Format
	token  string
	body   []byte
	err    error
}

func (s *subscriptionStub) Render(_ context.Context, token string, format subscription.Format) ([]byte, error) {
	s.token, s.format = token, format
	return s.body, s.err
}

func TestSubscriptionHandlerNegotiatesAndDoesNotCache(t *testing.T) {
	service := &subscriptionStub{body: []byte(`{"outbounds":[]}`)}
	handler := NewHandlerWithSubscription(nil, service)
	request := httptest.NewRequest(http.MethodGet, "/sub/AbCd?format=sing-box", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.token != "AbCd" || service.format != subscription.FormatSingBox {
		t.Fatalf("unexpected response: %d token=%q format=%q", response.Code, service.token, service.format)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control = %q", got)
	}
}

func TestSubscriptionHandlerMasksErrors(t *testing.T) {
	service := &subscriptionStub{err: errors.New("secret database detail")}
	handler := NewHandlerWithSubscription(nil, service)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sub/token", nil))
	if response.Code != http.StatusNotFound || response.Body.String() == "secret database detail" {
		t.Fatalf("unexpected response: %d %q", response.Code, response.Body.String())
	}
}

func TestSubscriptionHandlerRejectsUnknownFormat(t *testing.T) {
	service := &subscriptionStub{}
	handler := NewHandlerWithSubscription(nil, service)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sub/token?format=nope", nil))
	if response.Code != http.StatusBadRequest || service.token != "" {
		t.Fatalf("unexpected response: %d token=%q", response.Code, service.token)
	}
}
