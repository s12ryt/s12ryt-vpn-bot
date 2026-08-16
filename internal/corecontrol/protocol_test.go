package corecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientSendsOnlyCheckAndRestartCommands(t *testing.T) {
	candidatePath := filepath.Join(t.TempDir(), ".config.json.candidate-123")
	var methods, paths, bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body := make([]byte, request.ContentLength)
		_, _ = request.Body.Read(body)
		methods = append(methods, request.Method)
		paths = append(paths, request.URL.Path)
		bodies = append(bodies, string(body))
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := &Client{httpClient: server.Client(), endpoint: server.URL}

	if err := client.Check(context.Background(), candidatePath); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if err := client.Restart(context.Background()); err != nil {
		t.Fatalf("Restart() error = %v", err)
	}
	if strings.Join(methods, ",") != "POST,POST" || strings.Join(paths, ",") != "/v1/check,/v1/restart" {
		t.Fatalf("methods=%#v paths=%#v", methods, paths)
	}
	var checkBody struct {
		CandidatePath string `json:"candidate_path"`
	}
	if json.Unmarshal([]byte(bodies[0]), &checkBody) != nil || checkBody.CandidatePath != candidatePath || bodies[1] != "" {
		t.Fatalf("bodies=%#v", bodies)
	}
}

func TestClientDoesNotExposeControllerResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "docker secret detail", http.StatusBadGateway)
	}))
	defer server.Close()
	client := &Client{httpClient: server.Client(), endpoint: server.URL}

	err := client.Restart(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "docker") {
		t.Fatalf("Restart() error = %v", err)
	}
}

func TestHandlerChecksOnlyManagedCandidateAndRestarts(t *testing.T) {
	directory := t.TempDir()
	activePath := filepath.Join(directory, "config.json")
	candidatePath := filepath.Join(directory, ".config.json.candidate-123")
	if err := os.WriteFile(candidatePath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := &controllerStub{}
	handler, err := NewHandler(activePath, controller)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	check := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{"candidate_path":"`+filepath.ToSlash(candidatePath)+`"}`))
	check.Header.Set("Content-Type", "application/json")
	checkResponse := httptest.NewRecorder()
	handler.ServeHTTP(checkResponse, check)
	if checkResponse.Code != http.StatusNoContent || len(controller.checked) != 1 {
		t.Fatalf("check status=%d checked=%#v body=%q", checkResponse.Code, controller.checked, checkResponse.Body.String())
	}
	restartResponse := httptest.NewRecorder()
	handler.ServeHTTP(restartResponse, httptest.NewRequest(http.MethodPost, "/v1/restart", nil))
	if restartResponse.Code != http.StatusNoContent || controller.restarts != 1 {
		t.Fatalf("restart status=%d restarts=%d", restartResponse.Code, controller.restarts)
	}
}

func TestHandlerRejectsPathEscapeUnknownFieldsAndWrongMethodsBeforeController(t *testing.T) {
	directory := t.TempDir()
	activePath := filepath.Join(directory, "config.json")
	controller := &controllerStub{}
	handler, err := NewHandler(activePath, controller)
	if err != nil {
		t.Fatal(err)
	}
	tests := []*http.Request{
		httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{"candidate_path":"`+filepath.ToSlash(filepath.Join(directory, "..", "escape"))+`"}`)),
		httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{"candidate_path":"x","extra":true}`)),
		httptest.NewRequest(http.MethodGet, "/v1/restart", nil),
		httptest.NewRequest(http.MethodPost, "/v1/unknown", nil),
	}
	for _, request := range tests {
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code < 400 {
			t.Fatalf("%s %s status=%d", request.Method, request.URL.Path, response.Code)
		}
	}
	if len(controller.checked) != 0 || controller.restarts != 0 {
		t.Fatalf("controller reached checked=%#v restarts=%d", controller.checked, controller.restarts)
	}
}

func TestHandlerMasksControllerErrors(t *testing.T) {
	directory := t.TempDir()
	activePath := filepath.Join(directory, "config.json")
	candidatePath := filepath.Join(directory, ".config.json.candidate-123")
	if err := os.WriteFile(candidatePath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(activePath, &controllerStub{checkErr: errors.New("secret key exposed")})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{"candidate_path":"`+filepath.ToSlash(candidatePath)+`"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

type controllerStub struct {
	checked  []string
	checkErr error
	restarts int
}

func (stub *controllerStub) Check(_ context.Context, path string) error {
	stub.checked = append(stub.checked, path)
	return stub.checkErr
}

func (stub *controllerStub) Restart(context.Context) error {
	stub.restarts++
	return nil
}
