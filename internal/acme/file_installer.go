package acme

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileInstaller installs a validated certificate chain and private key at the
// configured absolute paths using temporary files in the same directory plus
// an atomic rename, so the sing-box core never observes a partial file.
type FileInstaller struct {
	certPath string
	keyPath  string
}

func NewFileInstaller(certPath, keyPath string) (*FileInstaller, error) {
	if !safeFileTarget(certPath) || !safeFileTarget(keyPath) {
		return nil, errors.New("certificate paths must be absolute and clean")
	}
	if filepath.Clean(certPath) == filepath.Clean(keyPath) {
		return nil, errors.New("certificate and key paths must differ")
	}
	return &FileInstaller{certPath: certPath, keyPath: keyPath}, nil
}

func (installer *FileInstaller) Install(_ context.Context, certificate Certificate) error {
	if installer == nil || installer.certPath == "" || installer.keyPath == "" {
		return errors.New("certificate installer is not initialized")
	}
	if !looksLikePEM(certificate.CertificatePEM, "CERTIFICATE") || !looksLikePEM(certificate.PrivateKeyPEM, "PRIVATE KEY") {
		return errors.New("certificate material is invalid")
	}
	if err := writeFileAtomically(installer.certPath, certificate.CertificatePEM, 0o644); err != nil {
		return fmt.Errorf("install certificate chain: %w", err)
	}
	if err := writeFileAtomically(installer.keyPath, certificate.PrivateKeyPEM, 0o600); err != nil {
		return fmt.Errorf("install private key: %w", err)
	}
	return nil
}

func safeFileTarget(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path
}

func looksLikePEM(material []byte, blockType string) bool {
	return bytes.Contains(material, []byte("-----BEGIN "+blockType)) &&
		bytes.Contains(material, []byte("-----END "+blockType)) &&
		strings.TrimSpace(string(material)) != ""
}

func writeFileAtomically(path string, contents []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}
