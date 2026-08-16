package singbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type CoreController interface {
	Check(ctx context.Context, candidatePath string) error
	Restart(ctx context.Context) error
}

type FileDeployment struct {
	activePath string
	directory  string
	baseName   string
	controller CoreController
	mutex      sync.Mutex
}

type filePromotion struct {
	BackupPath  string `json:"backup_path"`
	HadPrevious bool   `json:"had_previous"`
}

func NewFileDeployment(activePath string, controller CoreController) (*FileDeployment, error) {
	if controller == nil || activePath == "" || !filepath.IsAbs(activePath) {
		return nil, errors.New("absolute active path and core controller are required")
	}
	cleaned := filepath.Clean(activePath)
	directory := filepath.Dir(cleaned)
	baseName := filepath.Base(cleaned)
	if baseName == "." || baseName == string(filepath.Separator) {
		return nil, errors.New("active configuration filename is invalid")
	}
	return &FileDeployment{activePath: cleaned, directory: directory, baseName: baseName, controller: controller}, nil
}

func (deployment *FileDeployment) Stage(_ context.Context, configuration []byte) (StagedConfig, error) {
	if err := deployment.validate(); err != nil {
		return "", err
	}
	if len(configuration) == 0 {
		return "", errors.New("configuration is required")
	}
	deployment.mutex.Lock()
	defer deployment.mutex.Unlock()
	file, err := os.CreateTemp(deployment.directory, deployment.candidatePrefix())
	if err != nil {
		return "", fmt.Errorf("create candidate configuration: %w", err)
	}
	path := file.Name()
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return "", fmt.Errorf("protect candidate configuration: %w", err)
	}
	if _, err := file.Write(configuration); err != nil {
		return "", fmt.Errorf("write candidate configuration: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("sync candidate configuration: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close candidate configuration: %w", err)
	}
	remove = false
	return StagedConfig(path), nil
}

func (deployment *FileDeployment) Check(ctx context.Context, candidate StagedConfig) error {
	if err := deployment.validateCandidate(string(candidate)); err != nil {
		return err
	}
	return deployment.controller.Check(ctx, string(candidate))
}

func (deployment *FileDeployment) Discard(_ context.Context, candidate StagedConfig) error {
	if err := deployment.validateCandidate(string(candidate)); err != nil {
		return err
	}
	if err := os.Remove(string(candidate)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("discard candidate configuration: %w", err)
	}
	return nil
}

func (deployment *FileDeployment) Promote(_ context.Context, candidate StagedConfig) (Promotion, error) {
	if err := deployment.validateCandidate(string(candidate)); err != nil {
		return "", err
	}
	deployment.mutex.Lock()
	defer deployment.mutex.Unlock()
	state := filePromotion{}
	if _, err := os.Stat(deployment.activePath); err == nil {
		backup, createErr := os.CreateTemp(deployment.directory, deployment.backupPrefix())
		if createErr != nil {
			return "", fmt.Errorf("reserve configuration backup: %w", createErr)
		}
		state.BackupPath = backup.Name()
		if closeErr := backup.Close(); closeErr != nil {
			_ = os.Remove(state.BackupPath)
			return "", fmt.Errorf("close configuration backup: %w", closeErr)
		}
		if removeErr := os.Remove(state.BackupPath); removeErr != nil {
			return "", fmt.Errorf("prepare configuration backup: %w", removeErr)
		}
		if renameErr := os.Rename(deployment.activePath, state.BackupPath); renameErr != nil {
			return "", fmt.Errorf("backup active configuration: %w", renameErr)
		}
		state.HadPrevious = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect active configuration: %w", err)
	}
	if err := os.Rename(string(candidate), deployment.activePath); err != nil {
		if state.HadPrevious {
			_ = os.Rename(state.BackupPath, deployment.activePath)
		}
		return "", fmt.Errorf("promote candidate configuration: %w", err)
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("encode configuration promotion: %w", err)
	}
	return Promotion(encoded), nil
}

func (deployment *FileDeployment) Restart(ctx context.Context) error {
	if err := deployment.validate(); err != nil {
		return err
	}
	return deployment.controller.Restart(ctx)
}

func (deployment *FileDeployment) Rollback(_ context.Context, promotion Promotion) error {
	state, err := deployment.decodePromotion(promotion)
	if err != nil {
		return err
	}
	deployment.mutex.Lock()
	defer deployment.mutex.Unlock()
	if err := os.Remove(deployment.activePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove failed active configuration: %w", err)
	}
	if state.HadPrevious {
		if err := os.Rename(state.BackupPath, deployment.activePath); err != nil {
			return fmt.Errorf("restore previous configuration: %w", err)
		}
	}
	return nil
}

func (deployment *FileDeployment) Finalize(_ context.Context, promotion Promotion) error {
	state, err := deployment.decodePromotion(promotion)
	if err != nil {
		return err
	}
	if !state.HadPrevious {
		return nil
	}
	if err := os.Remove(state.BackupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove configuration backup: %w", err)
	}
	return nil
}

func (deployment *FileDeployment) validate() error {
	if deployment == nil || deployment.controller == nil || deployment.activePath == "" || deployment.directory == "" {
		return errors.New("file deployment is not initialized")
	}
	return nil
}

func (deployment *FileDeployment) validateCandidate(path string) error {
	if err := deployment.validate(); err != nil {
		return err
	}
	if !deployment.managedPath(path, deployment.candidatePrefix()) {
		return errors.New("candidate configuration path is invalid")
	}
	return nil
}

func (deployment *FileDeployment) decodePromotion(promotion Promotion) (filePromotion, error) {
	if err := deployment.validate(); err != nil {
		return filePromotion{}, err
	}
	var state filePromotion
	if len(promotion) == 0 || json.Unmarshal([]byte(promotion), &state) != nil {
		return filePromotion{}, errors.New("configuration promotion is invalid")
	}
	if state.HadPrevious != (state.BackupPath != "") ||
		(state.HadPrevious && !deployment.managedPath(state.BackupPath, deployment.backupPrefix())) {
		return filePromotion{}, errors.New("configuration promotion is invalid")
	}
	return state, nil
}

func (deployment *FileDeployment) managedPath(path string, prefix string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	cleaned := filepath.Clean(path)
	return filepath.Dir(cleaned) == deployment.directory && strings.HasPrefix(filepath.Base(cleaned), prefix)
}

func (deployment *FileDeployment) candidatePrefix() string {
	return "." + deployment.baseName + ".candidate-"
}

func (deployment *FileDeployment) backupPrefix() string {
	return "." + deployment.baseName + ".backup-"
}
