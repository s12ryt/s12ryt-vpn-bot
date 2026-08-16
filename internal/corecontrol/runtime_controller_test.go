package corecontrol

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeControllerChecksWithFixedBinaryAndArguments(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "sing-box")
	candidate := filepath.Join(t.TempDir(), ".config.json.candidate-1")
	runner := &commandRunnerStub{}
	controller := newTestRuntimeController(binary, runner, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusNoContent, ""), nil
	}))

	if err := controller.Check(context.Background(), candidate); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if runner.binary != binary || strings.Join(runner.arguments, "|") != "check|-c|"+candidate {
		t.Fatalf("binary=%q arguments=%#v", runner.binary, runner.arguments)
	}
}

func TestRuntimeControllerRestartsOnlyConfiguredContainer(t *testing.T) {
	var method, path, query string
	controller := newTestRuntimeController(filepath.Join(t.TempDir(), "sing-box"), &commandRunnerStub{}, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		method, path, query = request.Method, request.URL.Path, request.URL.RawQuery
		return response(http.StatusNoContent, ""), nil
	}))

	if err := controller.Restart(context.Background()); err != nil {
		t.Fatalf("Restart() error = %v", err)
	}
	if method != http.MethodPost || path != "/containers/sing-box/restart" || query != "t=10" {
		t.Fatalf("method=%q path=%q query=%q", method, path, query)
	}
}

func TestRuntimeControllerMasksCommandAndDockerErrors(t *testing.T) {
	runner := &commandRunnerStub{err: errors.New("private key from process")}
	controller := newTestRuntimeController(filepath.Join(t.TempDir(), "sing-box"), runner, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusInternalServerError, "docker secret"), nil
	}))

	if err := controller.Check(context.Background(), filepath.Join(t.TempDir(), "candidate")); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("Check() error = %v", err)
	}
	if err := controller.Restart(context.Background()); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("Restart() error = %v", err)
	}
}

func TestNewRuntimeControllerRejectsUnsafeContainerName(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "sing-box")
	socket := filepath.Join(t.TempDir(), "docker.sock")
	for _, name := range []string{"", "../other", "name/other", "space name"} {
		if _, err := NewRuntimeController(binary, socket, name); err == nil {
			t.Fatalf("NewRuntimeController() accepted %q", name)
		}
	}
}

func newTestRuntimeController(binary string, runner CommandRunner, transport http.RoundTripper) *RuntimeController {
	return &RuntimeController{
		singBoxBinary:  binary,
		containerName:  "sing-box",
		runner:         runner,
		dockerClient:   &http.Client{Transport: transport},
		dockerEndpoint: "http://docker",
	}
}

type commandRunnerStub struct {
	binary    string
	arguments []string
	err       error
}

func (stub *commandRunnerStub) Run(_ context.Context, binary string, arguments ...string) error {
	stub.binary = binary
	stub.arguments = append([]string(nil), arguments...)
	return stub.err
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
