package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/acme"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/tlsrunner"
)

type tlsSettingsLoader interface {
	LoadForIssuance(context.Context) (acme.Settings, time.Time, error)
}

type tlsRuntimeDependencies struct {
	core     coreSettingsLoader
	settings tlsSettingsLoader
	issuer   acme.Issuer
	issuance tlsrunner.IssuanceRecorder
	failures tlsrunner.FailureRecorder
	now      func() time.Time
}

// reloadingCertificateInstaller reads the certificate paths from the current
// core settings on every install, so owners can change TLS file locations
// without restarting the application.
type reloadingCertificateInstaller struct {
	core coreSettingsLoader
}

func (installer *reloadingCertificateInstaller) Install(ctx context.Context, certificate acme.Certificate) error {
	if installer == nil || installer.core == nil {
		return errors.New("certificate path provider is required")
	}
	settings, err := installer.core.Load(ctx)
	if err != nil {
		return fmt.Errorf("load core settings for certificate paths: %w", err)
	}
	fileInstaller, err := acme.NewFileInstaller(settings.TLSCertificatePath, settings.TLSKeyPath)
	if err != nil {
		return err
	}
	return fileInstaller.Install(ctx, certificate)
}

func buildTLSRuntime(dependencies tlsRuntimeDependencies) (*tlsrunner.Coordinator, error) {
	if dependencies.core == nil || dependencies.settings == nil || dependencies.issuer == nil ||
		dependencies.issuance == nil || dependencies.failures == nil {
		return nil, errors.New("TLS runtime dependencies are required")
	}
	if dependencies.now == nil {
		dependencies.now = time.Now
	}
	service, err := acme.NewService(dependencies.issuer, &reloadingCertificateInstaller{core: dependencies.core}, dependencies.now)
	if err != nil {
		return nil, err
	}
	return tlsrunner.NewCoordinator(dependencies.settings, service, dependencies.issuance, dependencies.failures, dependencies.now)
}
