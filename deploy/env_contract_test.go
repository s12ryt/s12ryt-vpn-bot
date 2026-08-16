package deploy

import (
	"os"
	"strings"
	"testing"
)

func TestEnvironmentExampleDocumentsRequiredBootstrapWithoutSecrets(t *testing.T) {
	body, err := os.ReadFile("../.env.example")
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}
	text := string(body)
	for _, required := range []string{
		"APP_MASTER_KEY=",
		"BOT_TOKEN=",
		"DATABASE_URL=",
		"OWNER_TG_ID=",
		"WEB_PUBLIC_URL=",
		"POSTGRES_PASSWORD=",
		"SINGBOX_IMAGE=",
		"DOCKER_GID=",
		"openssl rand -base64 32",
		"immutable version or digest",
	} {
		if !strings.Contains(text, required) {
			t.Errorf(".env.example missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"123456789:AA", "duckdns.org?token=", "BEGIN PRIVATE KEY",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf(".env.example contains secret-like value %q", forbidden)
		}
	}
}

func TestComposeRequiresExplicitDatabaseURL(t *testing.T) {
	body, err := os.ReadFile("../compose.yaml")
	if err != nil {
		t.Fatalf("read compose: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "DATABASE_URL: ${DATABASE_URL:?set DATABASE_URL}") {
		t.Fatal("app must require an explicit DATABASE_URL")
	}
	if strings.Contains(text, "DATABASE_URL: postgresql://${POSTGRES_USER") {
		t.Fatal("compose must not interpolate an unescaped password into DATABASE_URL")
	}
}
