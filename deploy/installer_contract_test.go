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
	if strings.Contains(contents, "BACKUP_RETENTION_DAYS") {
		t.Fatal("installer must not restore the obsolete environment-controlled backup retention policy")
	}
}

func TestInstallerCrossChecksAndConfirmsPublicAddressesBeforeStarting(t *testing.T) {
	contents := string(readRepositoryFile(t, filepath.Join("scripts", "install.sh")))
	for _, required := range []string{
		"https://api.ipify.org",
		"https://ifconfig.co/ip",
		"https://icanhazip.com",
		"detect_public_address",
		"confirm_public_addresses",
		"PUBLIC_IPV4",
		"PUBLIC_IPV6",
		"至少兩個外部來源",
		"確認公開位址",
		"公開 IPv${family}",
		"prompt_public_address 4",
		"prompt_public_address 6",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("installer missing public address confirmation contract %q", required)
		}
	}

	confirmation := strings.LastIndex(contents, "confirm_public_addresses")
	composeValidation := strings.Index(contents, "docker compose config --quiet")
	composeStart := strings.Index(contents, "docker compose up -d")
	if confirmation < 0 || composeValidation < 0 || composeStart < 0 || confirmation > composeValidation || confirmation > composeStart {
		t.Fatal("public addresses must be confirmed before Compose validation and startup")
	}
}

func TestInstallerUsesDetectedDefaultsAndConfirmsExistingEnvironment(t *testing.T) {
	contents := string(readRepositoryFile(t, filepath.Join("scripts", "install.sh")))
	for _, required := range []string{
		`input="${input:-$detected}"`,
		`[[ "$input" == '-' ]]`,
		"Enter 採用",
		"輸入 - 停用",
		"confirm_configured_public_addresses",
		`valid_ipv4 "$PUBLIC_IPV4"`,
		`valid_ipv6 "$PUBLIC_IPV6"`,
		"確認既有 .env 公開位址並繼續",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("installer missing public address reuse contract %q", required)
		}
	}

	newEnvironment := strings.Index(contents, "if [[ ! -f .env ]]; then")
	newConfirmation := strings.Index(contents[newEnvironment:], "confirm_public_addresses")
	existingConfirmation := strings.LastIndex(contents, "confirm_configured_public_addresses")
	composeValidation := strings.Index(contents, "docker compose config --quiet")
	if newEnvironment < 0 || newConfirmation < 0 || existingConfirmation < 0 || composeValidation < 0 {
		t.Fatal("installer must handle both new and existing environment address confirmation")
	}
	if newEnvironment+newConfirmation > composeValidation || existingConfirmation > composeValidation {
		t.Fatal("both new and existing environment addresses must be confirmed before Compose validation")
	}
}

func TestInstallerPreflightsBotACMEAndWebHTTPSTopology(t *testing.T) {
	contents := string(readRepositoryFile(t, filepath.Join("scripts", "install.sh")))
	for _, required := range []string{
		"verify_bot_identity",
		"getMe",
		"jq",
		"collect_acme_preflight",
		"ACME_MODE_REFERENCE",
		"ACME_DOMAIN_REFERENCE",
		"ACME_CHALLENGE_REFERENCE",
		"ACME_TERMS_ACCEPTED_REFERENCE",
		"https://letsencrypt.org/repository/",
		"https://zerossl.com/terms/",
		"sslip_io／duckdns／custom",
		"collect_web_https_topology",
		"WEB_HTTPS_TOPOLOGY",
		"WEB_PROXY_IP",
		"second_ip／custom_port／cloudflare_tunnel",
		"自訂 HTTPS port 不可為 443",
		"自訂 HTTPS port 與既有 TCP 服務衝突",
		"第二 IP 必須與 VPN 公開位址不同",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("installer missing Bot/ACME/Web HTTPS preflight contract %q", required)
		}
	}
	if strings.Contains(contents, "read -r -p 'DuckDNS token") {
		t.Fatal("installer must not collect the DuckDNS token outside encrypted Web settings")
	}
	if !strings.Contains(contents, `[[ "$ACME_CHALLENGE_REFERENCE" == http_01 ]] || fail "自有網域目前只支援 HTTP-01"`) {
		t.Fatal("installer must reject unsupported custom-domain DNS-01 preflight")
	}
	if strings.Contains(contents, "ACME challenge（http_01／dns_01）") {
		t.Fatal("installer must not offer custom DNS-01 without persisted provider credentials")
	}

	preflight := strings.LastIndex(contents, "run_installation_preflight")
	composeValidation := strings.Index(contents, "docker compose config --quiet")
	composeStart := strings.Index(contents, "docker compose up -d")
	if preflight < 0 || composeValidation < 0 || composeStart < 0 || preflight > composeValidation || preflight > composeStart {
		t.Fatal("Bot, ACME and Web HTTPS preflight must finish before Compose validation and startup")
	}
}

func TestInstallerValidatesBootstrapValuesBeforeBotNetworkCall(t *testing.T) {
	contents := string(readRepositoryFile(t, filepath.Join("scripts", "install.sh")))
	for _, required := range []string{
		"validate_bootstrap_inputs",
		`^[0-9]+:[A-Za-z0-9_-]{20,}$`,
		"parse_web_public_url",
		`^[A-Za-z0-9./_-]+(:v[0-9][A-Za-z0-9._-]*|@sha256:[a-f0-9]{64})$`,
		"bootstrap 值含有不安全字元",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("installer missing bootstrap validation contract %q", required)
		}
	}

	validation := strings.LastIndex(contents, "validate_bootstrap_inputs")
	preflight := strings.LastIndex(contents, "run_installation_preflight")
	if validation < 0 || preflight < 0 || validation > preflight {
		t.Fatal("bootstrap values must be validated before Bot and deployment preflight network calls")
	}
}

func TestInstallerRemovesBotTokenTemporaryFilesOnInterruption(t *testing.T) {
	contents := string(readRepositoryFile(t, filepath.Join("scripts", "install.sh")))
	for _, required := range []string{
		"INSTALL_TEMP_DIR",
		"trap cleanup_install_temp EXIT",
		"trap 'exit 130' INT",
		"trap 'exit 129' HUP",
		"trap 'exit 143' TERM",
		`config="${INSTALL_TEMP_DIR}/telegram-get-me.curl"`,
		`response="${INSTALL_TEMP_DIR}/telegram-get-me.json"`,
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("installer missing Bot token temporary-file cleanup contract %q", required)
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
