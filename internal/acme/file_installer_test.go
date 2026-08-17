package acme

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFileInstallerWritesCertificateAndKeyAtomically(t *testing.T) {
	directory := t.TempDir()
	certPath := filepath.Join(directory, "fullchain.pem")
	keyPath := filepath.Join(directory, "privkey.pem")
	installer, err := NewFileInstaller(certPath, keyPath)
	if err != nil {
		t.Fatalf("NewFileInstaller() error = %v", err)
	}
	certificate := certificateFixture(t, "vpn.example.com", time.Now().Add(-time.Minute), time.Now().Add(90*24*time.Hour))

	if err := installer.Install(context.Background(), certificate); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil || !strings.Contains(string(certPEM), "BEGIN CERTIFICATE") {
		t.Fatalf("certificate file = %q err=%v", certPEM, err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil || !strings.Contains(string(keyPEM), "PRIVATE KEY") {
		t.Fatalf("key file = %q err=%v", keyPEM, err)
	}
	if runtime.GOOS != "windows" {
		keyInfo, statErr := os.Stat(keyPath)
		if statErr != nil || keyInfo.Mode().Perm() != 0o600 {
			t.Fatalf("key mode = %v err=%v", keyInfo.Mode(), statErr)
		}
	}
	matches, _ := filepath.Glob(filepath.Join(directory, "*.tmp-*"))
	if len(matches) != 0 {
		t.Fatalf("temporary files left behind: %v", matches)
	}
}

func TestFileInstallerRejectsUnsafePathsAndEmptyMaterial(t *testing.T) {
	directory := t.TempDir()
	for _, invalid := range [][2]string{
		{"", filepath.Join(directory, "privkey.pem")},
		{filepath.Join(directory, "fullchain.pem"), ""},
		{"relative/fullchain.pem", filepath.Join(directory, "privkey.pem")},
		{directory + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "escape.pem", filepath.Join(directory, "privkey.pem")},
	} {
		if _, err := NewFileInstaller(invalid[0], invalid[1]); err == nil {
			t.Fatalf("expected rejection for %v", invalid)
		}
	}
	installer, err := NewFileInstaller(filepath.Join(directory, "fullchain.pem"), filepath.Join(directory, "privkey.pem"))
	if err != nil {
		t.Fatalf("NewFileInstaller() error = %v", err)
	}
	if err := installer.Install(context.Background(), Certificate{}); err == nil {
		t.Fatal("expected rejection for empty certificate material")
	}
	if err := installer.Install(context.Background(), Certificate{CertificatePEM: []byte("not pem"), PrivateKeyPEM: []byte("not pem")}); err == nil {
		t.Fatal("expected rejection for non-PEM material")
	}
	if _, err := os.Stat(filepath.Join(directory, "fullchain.pem")); !os.IsNotExist(err) {
		t.Fatal("rejected material must not leave files behind")
	}
}
