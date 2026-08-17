package deploy_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerChecksHostAndValidatesComposeBeforeStarting(t *testing.T) {
	contents := string(readRepositoryFile(t, filepath.Join("scripts", "install.sh")))
	for _, required := range []string{
		"set -Eeuo pipefail", "uname -s", "x86_64", "aarch64", "docker compose version",
		"APP_MASTER_KEY", "BOT_TOKEN", "OWNER_TG_ID", "WEB_PUBLIC_URL", "DATABASE_URL",
		"SINGBOX_IMAGE", "DOCKER_GID", "docker compose config --quiet", "docker compose up -d",
		"if ! docker compose pull", "read:packages", "套件可見度",
		"TCP 443", "UDP 443", "UDP 8443", "TCP 8443", "127.0.0.1:35699",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("installer missing %q", required)
		}
	}
	for _, forbidden := range []string{"echo $BOT_TOKEN", "echo ${BOT_TOKEN}", "echo $APP_MASTER_KEY", "echo ${APP_MASTER_KEY}"} {
		if strings.Contains(contents, forbidden) {
			t.Errorf("installer must not print secrets: %q", forbidden)
		}
	}
}

func TestPostDeployCheckCoversRealIntegrationBoundaries(t *testing.T) {
	contents := string(readRepositoryFile(t, filepath.Join("scripts", "post-deploy-check.sh")))
	for _, required := range []string{
		"set -Eeuo pipefail", "VERIFY_SUBSCRIPTION_URL", "VERIFY_QUALIFICATION_CHAT_ID",
		"getMe", "getChatMember", "/health/live", "/health/ready", "openssl s_client",
		"docker compose ps", "s12ryt-sing-box", "core-control.sock", "traffic/pending.json",
		"sing-box", "Hysteria2", "TUIC", "AnyTLS", "IPv6-only", "配額", "重啟",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("post-deploy checker missing %q", required)
		}
	}
	if strings.Contains(contents, "set -x") {
		t.Fatal("post-deploy checker must not enable shell tracing with secrets")
	}
}

func TestPostDeployCheckCannotReportCompleteWithoutExternalEvidence(t *testing.T) {
	contents := string(readRepositoryFile(t, filepath.Join("scripts", "post-deploy-check.sh")))
	for _, required := range []string{
		"VERIFY_EXTERNAL_EVIDENCE_FILE",
		"protocols_dual_stack",
		"ipv6_only_egress",
		"ipv4_enabled_egress",
		"traffic_accounting",
		"quota_enforcement",
		"period_recovery",
		"restart_behavior",
		"concurrent_connections_600",
		"外部驗收證據不完整",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("post-deploy checker missing external evidence gate %q", required)
		}
	}
	if strings.Contains(contents, "需人工／負載環境續驗") {
		t.Fatal("post-deploy checker must fail closed instead of succeeding with an incomplete verification notice")
	}
}

func TestPostDeployCheckVerifiesReferencedEvidenceFiles(t *testing.T) {
	contents := string(readRepositoryFile(t, filepath.Join("scripts", "post-deploy-check.sh")))
	for _, required := range []string{
		"realpath -e",
		"evidence_root",
		"evidence_path",
		"外部驗收 evidence 路徑不安全",
		"外部驗收 evidence 檔案不存在",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("post-deploy checker missing evidence file verification %q", required)
		}
	}
}
