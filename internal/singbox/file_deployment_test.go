package singbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileDeploymentInstallerAtomicallyPromotesCheckedConfiguration(t *testing.T) {
	directory := t.TempDir()
	activePath := filepath.Join(directory, "config.json")
	if err := os.WriteFile(activePath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := &coreControllerStub{}
	deployment, err := NewFileDeployment(activePath, controller)
	if err != nil {
		t.Fatalf("NewFileDeployment() error = %v", err)
	}

	if err := NewInstaller(deployment).Install(context.Background(), []byte("new")); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	content, err := os.ReadFile(activePath)
	if err != nil || string(content) != "new" {
		t.Fatalf("active content=%q error=%v", content, err)
	}
	if len(controller.checkedPaths) != 1 || controller.restarts != 1 {
		t.Fatalf("checked=%#v restarts=%d", controller.checkedPaths, controller.restarts)
	}
	if matches, _ := filepath.Glob(filepath.Join(directory, ".config.json.*")); len(matches) != 0 {
		t.Fatalf("temporary files remain: %#v", matches)
	}
}

func TestFileDeploymentRestoresPreviousFileWhenRestartFails(t *testing.T) {
	directory := t.TempDir()
	activePath := filepath.Join(directory, "config.json")
	if err := os.WriteFile(activePath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("restart rejected")
	controller := &coreControllerStub{restartErrors: []error{wantErr, nil}}
	deployment, err := NewFileDeployment(activePath, controller)
	if err != nil {
		t.Fatal(err)
	}

	err = NewInstaller(deployment).Install(context.Background(), []byte("new"))
	if !errors.Is(err, wantErr) || InstallFailureStage(err) != InstallStageRestart {
		t.Fatalf("Install() error=%v stage=%q", err, InstallFailureStage(err))
	}
	content, readErr := os.ReadFile(activePath)
	if readErr != nil || string(content) != "old" {
		t.Fatalf("restored content=%q error=%v", content, readErr)
	}
	if controller.restarts != 2 {
		t.Fatalf("restart calls=%d", controller.restarts)
	}
}

func TestFileDeploymentDiscardsCandidateWhenControllerCheckFails(t *testing.T) {
	directory := t.TempDir()
	activePath := filepath.Join(directory, "config.json")
	wantErr := errors.New("invalid config")
	controller := &coreControllerStub{checkErr: wantErr}
	deployment, err := NewFileDeployment(activePath, controller)
	if err != nil {
		t.Fatal(err)
	}

	err = NewInstaller(deployment).Install(context.Background(), []byte("invalid"))
	if !errors.Is(err, wantErr) || InstallFailureStage(err) != InstallStageCheck {
		t.Fatalf("Install() error=%v stage=%q", err, InstallFailureStage(err))
	}
	if _, statErr := os.Stat(activePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("active file unexpectedly exists: %v", statErr)
	}
	if matches, _ := filepath.Glob(filepath.Join(directory, ".config.json.*")); len(matches) != 0 {
		t.Fatalf("candidate remains: %#v", matches)
	}
}

func TestNewFileDeploymentRejectsRelativePathAndNilController(t *testing.T) {
	if _, err := NewFileDeployment("config.json", &coreControllerStub{}); err == nil {
		t.Fatal("NewFileDeployment() accepted relative path")
	}
	if _, err := NewFileDeployment(filepath.Join(t.TempDir(), "config.json"), nil); err == nil {
		t.Fatal("NewFileDeployment() accepted nil controller")
	}
}

type coreControllerStub struct {
	checkedPaths  []string
	checkErr      error
	restartErrors []error
	restarts      int
}

func (stub *coreControllerStub) Check(_ context.Context, path string) error {
	stub.checkedPaths = append(stub.checkedPaths, path)
	return stub.checkErr
}

func (stub *coreControllerStub) Restart(context.Context) error {
	stub.restarts++
	if len(stub.restartErrors) == 0 {
		return nil
	}
	err := stub.restartErrors[0]
	stub.restartErrors = stub.restartErrors[1:]
	return err
}
