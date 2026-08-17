package deploy_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLicenseIsCanonicalAGPLVersion3(t *testing.T) {
	contents := readRepositoryFile(t, "LICENSE")
	if got := fmt.Sprintf("%x", sha256.Sum256(contents)); got != "0d96a4ff68ad6d4b6f1f30f713b18d5184912ba8dd389f86aa7710db079abcb0" {
		t.Fatalf("LICENSE must be the canonical GNU AGPL-3.0 text, got sha256 %s", got)
	}
}

func TestReadmeDoesNotPresentDevelopmentBuildAsProductionReady(t *testing.T) {
	contents := string(readRepositoryFile(t, "README.md"))
	for _, required := range []string{
		"開發中",
		"尚未完成",
		"請勿直接用於正式環境",
		"GNU Affero General Public License v3.0 only",
		"本程式不提供任何擔保",
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("README.md must contain %q", required)
		}
	}
}

func TestReadmeListsBothRemainingExternalBlockers(t *testing.T) {
	contents := string(readRepositoryFile(t, "README.md"))
	for _, required := range []string{
		"上游 sing-box stable 依賴漏洞阻擋",
		"實際 Linux 主機",
		"真實 Telegram",
		"ACME",
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("README.md must identify remaining external blocker %q", required)
		}
	}

	for _, stale := range []string{
		"安裝精靈、ACME 自動化、備份／還原、完整設定頁",
		"GitHub Actions 仍需在 repository 建立後實際執行",
	} {
		if strings.Contains(contents, stale) {
			t.Fatalf("README.md must not retain completed-work claim %q", stale)
		}
	}
}

func TestReverseProxyGuideCoversSupportedTopologies(t *testing.T) {
	contents := string(readRepositoryFile(t, filepath.Join("docs", "reverse-proxy.md")))
	for _, required := range []string{"Nginx", "Caddy", "Cloudflare Tunnel", "127.0.0.1:35699", "TCP 443", "X-Forwarded-For", "TRUSTED_PROXY_CIDRS"} {
		if !strings.Contains(contents, required) {
			t.Errorf("reverse proxy guide missing %q", required)
		}
	}
	if strings.Contains(contents, "/var/run/docker.sock") {
		t.Fatal("reverse proxy guide must not expose the Docker socket")
	}
}

func TestInstallationGuideSeparatesAutomatedAndManualAcceptance(t *testing.T) {
	contents := string(readRepositoryFile(t, filepath.Join("docs", "installation.md")))
	for _, required := range []string{
		"scripts/install.sh", "scripts/post-deploy-check.sh", "VERIFY_SUBSCRIPTION_URL",
		"VERIFY_QUALIFICATION_CHAT_ID", "VERIFY_TLS_SERVER_NAME", "600", "未完整驗證",
		"docs/reverse-proxy.md", "docs/backup-restore.md",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("installation guide missing %q", required)
		}
	}
}

func readRepositoryFile(t *testing.T, name string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return contents
}

func TestReleasePublicationVisibilityIsDocumented(t *testing.T) {
	readme, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	if !strings.Contains(string(readme), "套件可見度") {
		t.Fatal("README must document that first-release packages default to private and how to publish them")
	}
	workflow, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	if !strings.Contains(string(workflow), "Change visibility") {
		t.Fatal("successful release runs must remind the owner to set package visibility")
	}
}
