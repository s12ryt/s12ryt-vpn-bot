package singbox

import (
	"context"
	"errors"
	"fmt"
)

type StagedConfig string
type Promotion string

type InstallStage string

const (
	InstallStageStage    InstallStage = "stage"
	InstallStageCheck    InstallStage = "check"
	InstallStagePromote  InstallStage = "promote"
	InstallStageRestart  InstallStage = "restart"
	InstallStageRollback InstallStage = "rollback"
	InstallStageFinalize InstallStage = "finalize"
)

type Deployment interface {
	Stage(ctx context.Context, configuration []byte) (StagedConfig, error)
	Check(ctx context.Context, candidate StagedConfig) error
	Discard(ctx context.Context, candidate StagedConfig) error
	Promote(ctx context.Context, candidate StagedConfig) (Promotion, error)
	Restart(ctx context.Context) error
	Rollback(ctx context.Context, promotion Promotion) error
	Finalize(ctx context.Context, promotion Promotion) error
}

type InstallError struct {
	Stage InstallStage
	Err   error
}

func (e *InstallError) Error() string {
	return fmt.Sprintf("sing-box install %s failed: %v", e.Stage, e.Err)
}

func (e *InstallError) Unwrap() error {
	return e.Err
}

func InstallFailureStage(err error) InstallStage {
	var installError *InstallError
	if errors.As(err, &installError) {
		return installError.Stage
	}
	return ""
}

type Installer struct {
	deployment Deployment
}

func NewInstaller(deployment Deployment) *Installer {
	return &Installer{deployment: deployment}
}

func (installer *Installer) Install(ctx context.Context, configuration []byte) error {
	if installer == nil || installer.deployment == nil {
		return &InstallError{Stage: InstallStageStage, Err: errors.New("deployment is required")}
	}
	if len(configuration) == 0 {
		return &InstallError{Stage: InstallStageStage, Err: errors.New("configuration is required")}
	}
	candidate, err := installer.deployment.Stage(ctx, configuration)
	if err != nil {
		return &InstallError{Stage: InstallStageStage, Err: err}
	}
	if err := installer.deployment.Check(ctx, candidate); err != nil {
		discardErr := installer.deployment.Discard(ctx, candidate)
		return &InstallError{Stage: InstallStageCheck, Err: errors.Join(err, discardErr)}
	}
	promotion, err := installer.deployment.Promote(ctx, candidate)
	if err != nil {
		discardErr := installer.deployment.Discard(ctx, candidate)
		return &InstallError{Stage: InstallStagePromote, Err: errors.Join(err, discardErr)}
	}
	if err := installer.deployment.Restart(ctx); err != nil {
		restartErr := err
		if rollbackErr := installer.deployment.Rollback(ctx, promotion); rollbackErr != nil {
			return &InstallError{Stage: InstallStageRollback, Err: errors.Join(restartErr, rollbackErr)}
		}
		if previousRestartErr := installer.deployment.Restart(ctx); previousRestartErr != nil {
			return &InstallError{Stage: InstallStageRollback, Err: errors.Join(restartErr, previousRestartErr)}
		}
		finalizeErr := installer.deployment.Finalize(ctx, promotion)
		return &InstallError{Stage: InstallStageRestart, Err: errors.Join(restartErr, finalizeErr)}
	}
	if err := installer.deployment.Finalize(ctx, promotion); err != nil {
		return &InstallError{Stage: InstallStageFinalize, Err: err}
	}
	return nil
}
