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
	environment := readBackupContractFile(t, filepath.Join(root, ".env.example"))
	guide := readBackupContractFile(t, filepath.Join(root, "docs", "backup-restore.md"))

	for _, required := range []string{"BACKUP_DIR", "NewBackupSettingsStore", "retentionForAttempt", "pg_dump", "PGDATABASE", "0600", "24 * time.Hour"} {
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
	backupSection := between(compose, "  backup:\n", "  core-controller:\n")
	if !strings.Contains(backupSection, "network_mode: host") {
		t.Fatal("backup must use host networking because DATABASE_URL targets the loopback-only PostgreSQL publication")
	}
	if strings.Contains(compose, "BACKUP_RETENTION_DAYS") || strings.Contains(environment, "BACKUP_RETENTION_DAYS") {
		t.Fatal("backup retention must be controlled by PostgreSQL and Web, not a startup environment variable")
	}
	for _, required := range []string{"APP_MASTER_KEY", "docker compose run --rm", "RESTORE_ARCHIVE", "pg_restore", "完整性", "Web 管理面板", "預設保留 7 天"} {
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
