package singbox

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestInstallerChecksPromotesRestartsAndFinalizesCandidate(t *testing.T) {
	deployment := &deploymentStub{}
	installer := NewInstaller(deployment)

	if err := installer.Install(context.Background(), []byte(`{"valid":true}`)); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	want := []string{"stage", "check:candidate", "promote:candidate", "restart", "finalize:promotion"}
	if !reflect.DeepEqual(deployment.calls, want) {
		t.Fatalf("calls = %#v, want %#v", deployment.calls, want)
	}
}

func TestInstallerDiscardsUncheckedCandidateAndPreservesCurrentConfiguration(t *testing.T) {
	wantCheckErr := errors.New("sing-box check failed")
	deployment := &deploymentStub{checkErr: wantCheckErr}
	installer := NewInstaller(deployment)

	err := installer.Install(context.Background(), []byte(`{"invalid":true}`))
	if !errors.Is(err, wantCheckErr) || InstallFailureStage(err) != InstallStageCheck {
		t.Fatalf("Install() error = %v stage=%q", err, InstallFailureStage(err))
	}
	want := []string{"stage", "check:candidate", "discard:candidate"}
	if !reflect.DeepEqual(deployment.calls, want) {
		t.Fatalf("calls = %#v, want %#v", deployment.calls, want)
	}
}

func TestInstallerRollsBackAndRestartsPreviousConfigurationOnRestartFailure(t *testing.T) {
	wantRestartErr := errors.New("restart failed")
	deployment := &deploymentStub{restartErrors: []error{wantRestartErr, nil}}
	installer := NewInstaller(deployment)

	err := installer.Install(context.Background(), []byte(`{"valid":true}`))
	if !errors.Is(err, wantRestartErr) || InstallFailureStage(err) != InstallStageRestart {
		t.Fatalf("Install() error = %v stage=%q", err, InstallFailureStage(err))
	}
	want := []string{
		"stage", "check:candidate", "promote:candidate", "restart",
		"rollback:promotion", "restart", "finalize:promotion",
	}
	if !reflect.DeepEqual(deployment.calls, want) {
		t.Fatalf("calls = %#v, want %#v", deployment.calls, want)
	}
}

func TestInstallerReportsRollbackFailureWithoutClaimingSuccess(t *testing.T) {
	wantRestartErr := errors.New("restart failed")
	wantRollbackErr := errors.New("rollback failed")
	deployment := &deploymentStub{restartErrors: []error{wantRestartErr}, rollbackErr: wantRollbackErr}
	installer := NewInstaller(deployment)

	err := installer.Install(context.Background(), []byte(`{"valid":true}`))
	if !errors.Is(err, wantRestartErr) || !errors.Is(err, wantRollbackErr) || InstallFailureStage(err) != InstallStageRollback {
		t.Fatalf("Install() error = %v stage=%q", err, InstallFailureStage(err))
	}
}

type deploymentStub struct {
	calls         []string
	stageErr      error
	checkErr      error
	promoteErr    error
	restartErrors []error
	rollbackErr   error
	finalizeErr   error
}

func (stub *deploymentStub) Stage(_ context.Context, _ []byte) (StagedConfig, error) {
	stub.calls = append(stub.calls, "stage")
	return StagedConfig("candidate"), stub.stageErr
}

func (stub *deploymentStub) Check(_ context.Context, candidate StagedConfig) error {
	stub.calls = append(stub.calls, "check:"+string(candidate))
	return stub.checkErr
}

func (stub *deploymentStub) Discard(_ context.Context, candidate StagedConfig) error {
	stub.calls = append(stub.calls, "discard:"+string(candidate))
	return nil
}

func (stub *deploymentStub) Promote(_ context.Context, candidate StagedConfig) (Promotion, error) {
	stub.calls = append(stub.calls, "promote:"+string(candidate))
	return Promotion("promotion"), stub.promoteErr
}

func (stub *deploymentStub) Restart(context.Context) error {
	stub.calls = append(stub.calls, "restart")
	if len(stub.restartErrors) == 0 {
		return nil
	}
	err := stub.restartErrors[0]
	stub.restartErrors = stub.restartErrors[1:]
	return err
}

func (stub *deploymentStub) Rollback(_ context.Context, promotion Promotion) error {
	stub.calls = append(stub.calls, "rollback:"+string(promotion))
	return stub.rollbackErr
}

func (stub *deploymentStub) Finalize(_ context.Context, promotion Promotion) error {
	stub.calls = append(stub.calls, "finalize:"+string(promotion))
	return stub.finalizeErr
}
