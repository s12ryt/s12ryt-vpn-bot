package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupAndRestoreDeploymentContract(t *testing.T) {
	root := filepath.Clean("..")
	backup := readBackupContractFile(t, filepath.Join(root, "cmd", "backup", "main.go"))
	restore := readBackupContractFile(t, filepath.Join(root, "cmd", "restore", "main.go"))
	dockerfile := readBackupContractFile(t, filepath.Join(root, "Dockerfile.backup"))
	compose := readBackupContractFile(t, filepath.Join(root, "compose.yaml"))
	guide := readBackupContractFile(t, filepath.Join(root, "docs", "backup-restore.md"))

	for _, required := range []string{"BACKUP_DIR", "BACKUP_RETENTION_DAYS", "pg_dump", "PGDATABASE", "0600", "24 * time.Hour"} {
		if !strings.Contains(backup, required) {
			t.Errorf("backup command missing %q", required)
		}
	}
	for _, required := range []string{"RESTORE_ARCHIVE", "pg_restore", "PGDATABASE"} {
		if !strings.Contains(restore, required) {
			t.Errorf("restore command missing %q", required)
		}
	}
	if strings.Contains(backup, `"--dbname", databaseURL`) || strings.Contains(restore, `"--dbname", databaseURL`) {
		t.Error("database URL must not be exposed in process arguments")
	}
	for _, required := range []string{"postgres:17.6-bookworm", "./cmd/backup", "./cmd/restore", "/usr/local/bin/backup", "/usr/local/bin/restore", "USER 65532:65532"} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("Dockerfile missing %q", required)
		}
	}
	for _, required := range []string{"backup:", "Dockerfile.backup", "/usr/local/bin/backup", "backups:/var/lib/s12ryt/backups", "read_only: true", "no-new-privileges:true"} {
		if !strings.Contains(compose, required) {
			t.Errorf("compose missing backup contract %q", required)
		}
	}
	for _, required := range []string{"APP_MASTER_KEY", "docker compose run --rm", "RESTORE_ARCHIVE", "pg_restore", "完整性", "7"} {
		if !strings.Contains(guide, required) {
			t.Errorf("backup guide missing %q", required)
		}
	}
}

func readBackupContractFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
