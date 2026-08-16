package corecontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxControlBody = 4096

type Client struct {
	httpClient *http.Client
	endpoint   string
}

func NewUnixClient(socketPath string) (*Client, error) {
	if socketPath == "" || !filepath.IsAbs(socketPath) {
		return nil, errors.New("absolute core control socket path is required")
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
		DisableKeepAlives: false,
	}
	return &Client{
		httpClient: &http.Client{Transport: transport, Timeout: 30 * time.Second},
		endpoint:   "http://core-control",
	}, nil
}

func (client *Client) Check(ctx context.Context, candidatePath string) error {
	if candidatePath == "" || !filepath.IsAbs(candidatePath) {
		return errors.New("absolute candidate path is required")
	}
	body, err := json.Marshal(struct {
		CandidatePath string `json:"candidate_path"`
	}{CandidatePath: candidatePath})
	if err != nil {
		return errors.New("encode core check request")
	}
	return client.post(ctx, "/v1/check", body, true)
}

func (client *Client) Restart(ctx context.Context) error {
	return client.post(ctx, "/v1/restart", nil, false)
}

func (client *Client) post(ctx context.Context, path string, body []byte, jsonBody bool) error {
	if client == nil || client.httpClient == nil || client.endpoint == "" {
		return errors.New("core control client is not initialized")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return errors.New("create core control request")
	}
	if jsonBody {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return errors.New("core control request failed")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxControlBody))
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("core control request rejected with status %d", response.StatusCode)
	}
	return nil
}

type Controller interface {
	Check(ctx context.Context, candidatePath string) error
	Restart(ctx context.Context) error
}

type Handler struct {
	activePath string
	directory  string
	prefix     string
	controller Controller
}

func NewHandler(activePath string, controller Controller) (*Handler, error) {
	if controller == nil || activePath == "" || !filepath.IsAbs(activePath) {
		return nil, errors.New("absolute active path and controller are required")
	}
	cleaned := filepath.Clean(activePath)
	base := filepath.Base(cleaned)
	return &Handler{
		activePath: cleaned,
		directory:  filepath.Dir(cleaned),
		prefix:     "." + base + ".candidate-",
		controller: controller,
	}, nil
}

func (handler *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if handler == nil || handler.controller == nil {
		http.Error(writer, "controller_unavailable", http.StatusServiceUnavailable)
		return
	}
	if request.Method != http.MethodPost {
		http.Error(writer, "method_not_allowed", http.StatusMethodNotAllowed)
		return
	}
	switch request.URL.Path {
	case "/v1/check":
		handler.handleCheck(writer, request)
	case "/v1/restart":
		if request.ContentLength > 0 {
			http.Error(writer, "request_invalid", http.StatusBadRequest)
			return
		}
		if err := handler.controller.Restart(request.Context()); err != nil {
			http.Error(writer, "core_restart_failed", http.StatusBadGateway)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(writer, request)
	}
}

func (handler *Handler) handleCheck(writer http.ResponseWriter, request *http.Request) {
	if contentType := request.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(contentType), "application/json") {
		http.Error(writer, "request_invalid", http.StatusUnsupportedMediaType)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxControlBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var payload struct {
		CandidatePath string `json:"candidate_path"`
	}
	if err := decoder.Decode(&payload); err != nil {
		http.Error(writer, "request_invalid", http.StatusBadRequest)
		return
	}
	if err := ensureJSONEnd(decoder); err != nil || !handler.managedCandidate(payload.CandidatePath) {
		http.Error(writer, "request_invalid", http.StatusBadRequest)
		return
	}
	if err := handler.controller.Check(request.Context(), filepath.Clean(payload.CandidatePath)); err != nil {
		http.Error(writer, "core_check_failed", http.StatusBadGateway)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (handler *Handler) managedCandidate(path string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	cleaned := filepath.Clean(path)
	if filepath.Dir(cleaned) != handler.directory || !strings.HasPrefix(filepath.Base(cleaned), handler.prefix) {
		return false
	}
	fileInfo, err := os.Lstat(cleaned)
	return err == nil && fileInfo.Mode().IsRegular()
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request has trailing JSON")
	}
	return nil
}
