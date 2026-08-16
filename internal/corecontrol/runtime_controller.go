package corecontrol

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"
)

var containerNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type CommandRunner interface {
	Run(ctx context.Context, binary string, arguments ...string) error
}

type RuntimeController struct {
	singBoxBinary  string
	containerName  string
	runner         CommandRunner
	dockerClient   *http.Client
	dockerEndpoint string
}

func NewRuntimeController(singBoxBinary, dockerSocket, containerName string) (*RuntimeController, error) {
	if !filepath.IsAbs(singBoxBinary) || !filepath.IsAbs(dockerSocket) || !containerNamePattern.MatchString(containerName) {
		return nil, errors.New("runtime controller configuration is invalid")
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", dockerSocket)
		},
	}
	return &RuntimeController{
		singBoxBinary:  filepath.Clean(singBoxBinary),
		containerName:  containerName,
		runner:         execCommandRunner{},
		dockerClient:   &http.Client{Transport: transport, Timeout: 30 * time.Second},
		dockerEndpoint: "http://docker",
	}, nil
}

func (controller *RuntimeController) Check(ctx context.Context, candidatePath string) error {
	if err := controller.validate(); err != nil {
		return err
	}
	if !filepath.IsAbs(candidatePath) {
		return errors.New("absolute candidate path is required")
	}
	if err := controller.runner.Run(ctx, controller.singBoxBinary, "check", "-c", filepath.Clean(candidatePath)); err != nil {
		return errors.New("sing-box configuration check failed")
	}
	return nil
}

func (controller *RuntimeController) Restart(ctx context.Context) error {
	if err := controller.validate(); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		controller.dockerEndpoint+"/containers/"+controller.containerName+"/restart?t=10", nil)
	if err != nil {
		return errors.New("create sing-box restart request")
	}
	response, err := controller.dockerClient.Do(request)
	if err != nil {
		return errors.New("sing-box restart request failed")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxControlBody))
	if response.StatusCode != http.StatusNoContent {
		return errors.New("sing-box restart was rejected")
	}
	return nil
}

func (controller *RuntimeController) validate() error {
	if controller == nil || controller.runner == nil || controller.dockerClient == nil ||
		!filepath.IsAbs(controller.singBoxBinary) || !containerNamePattern.MatchString(controller.containerName) ||
		controller.dockerEndpoint == "" {
		return errors.New("runtime controller is not initialized")
	}
	return nil
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, binary string, arguments ...string) error {
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run()
}
