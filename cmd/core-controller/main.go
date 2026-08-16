package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/corecontrol"
)

var safeContainerName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type controllerConfig struct {
	controlSocket string
	activeConfig  string
	singBoxBinary string
	dockerSocket  string
	containerName string
}

func main() {
	if err := run(); err != nil {
		log.Printf("core controller stopped: %v", err)
		os.Exit(1)
	}
}

func run() error {
	configuration, err := loadControllerConfig(os.Getenv)
	if err != nil {
		return err
	}
	controller, err := corecontrol.NewRuntimeController(
		configuration.singBoxBinary,
		configuration.dockerSocket,
		configuration.containerName,
	)
	if err != nil {
		return fmt.Errorf("initialize runtime controller: %w", err)
	}
	handler, err := corecontrol.NewHandler(configuration.activeConfig, controller)
	if err != nil {
		return fmt.Errorf("initialize control handler: %w", err)
	}
	listener, err := listenUnix(configuration.controlSocket)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(configuration.controlSocket)

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       time.Minute,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverError := make(chan error, 1)
	go func() {
		serverError <- server.Serve(listener)
	}()
	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownContext)
	case err := <-serverError:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve core control socket: %w", err)
	}
}

func loadControllerConfig(getenv func(string) string) (controllerConfig, error) {
	configuration := controllerConfig{
		controlSocket: valueOrDefault(getenv("CORE_CONTROL_SOCKET"), "/run/s12ryt/core-control.sock"),
		activeConfig:  valueOrDefault(getenv("SINGBOX_CONFIG_PATH"), "/var/lib/s12ryt/sing-box/config.json"),
		singBoxBinary: valueOrDefault(getenv("SINGBOX_BINARY"), "/usr/local/bin/sing-box"),
		dockerSocket:  valueOrDefault(getenv("DOCKER_SOCKET"), "/var/run/docker.sock"),
		containerName: valueOrDefault(getenv("SINGBOX_CONTAINER"), "s12ryt-sing-box"),
	}
	if !path.IsAbs(configuration.controlSocket) || !path.IsAbs(configuration.activeConfig) ||
		!path.IsAbs(configuration.singBoxBinary) || !path.IsAbs(configuration.dockerSocket) ||
		!safeContainerName.MatchString(configuration.containerName) {
		return controllerConfig{}, errors.New("core controller environment is invalid")
	}
	return configuration, nil
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func listenUnix(socketPath string) (net.Listener, error) {
	directory := filepath.Dir(socketPath)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return nil, fmt.Errorf("create core control socket directory: %w", err)
	}
	if info, err := os.Lstat(socketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("core control socket path is occupied by a non-socket file")
		}
		if err := os.Remove(socketPath); err != nil {
			return nil, fmt.Errorf("remove stale core control socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect core control socket: %w", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on core control socket: %w", err)
	}
	if err := os.Chmod(socketPath, 0o660); err != nil {
		listener.Close()
		_ = os.Remove(socketPath)
		return nil, fmt.Errorf("protect core control socket: %w", err)
	}
	return listener, nil
}
