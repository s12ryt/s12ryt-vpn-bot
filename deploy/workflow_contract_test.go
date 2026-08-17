package deploy

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestCIWorkflowCoversRequiredVerification(t *testing.T) {
	body, err := os.ReadFile("../.github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	workflow := string(body)
	for _, required := range []string{
		"permissions:\n  contents: read",
		"go test -race ./...",
		"go vet ./...",
		"go test -race -tags=integration ./integration",
		"npm ci",
		"npm test -- --run",
		"npm run lint",
		"npm run build",
		"docker compose config --quiet",
		"shellcheck scripts/install.sh scripts/post-deploy-check.sh",
		"docker buildx build",
		"-f Dockerfile.backup -t s12ryt-vpn-backup:ci",
		"services:\n      postgres:",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("CI workflow missing %q", required)
		}
	}

	action := regexp.MustCompile(`uses: [A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+@([0-9a-f]{40})(?:\s+#\s+v[0-9]+)?`)
	usesCount := strings.Count(workflow, "uses: ")
	if matches := action.FindAllStringSubmatch(workflow, -1); len(matches) != usesCount || usesCount == 0 {
		t.Fatalf("all third-party actions must be pinned to full commit SHAs: uses=%d pinned=%d", usesCount, len(matches))
	}
}

func TestPostgresIntegrationTestExists(t *testing.T) {
	body, err := os.ReadFile("../integration/postgres_migration_test.go")
	if err != nil {
		t.Fatalf("read PostgreSQL integration test: %v", err)
	}
	text := string(body)
	for _, required := range []string{"//go:build integration", "postgres.Migrate", "schema_migrations"} {
		if !strings.Contains(text, required) {
			t.Errorf("PostgreSQL integration test missing %q", required)
		}
	}
}

func TestTrafficScaleIntegrationTestExists(t *testing.T) {
	body, err := os.ReadFile("../integration/traffic_scale_test.go")
	if err != nil {
		t.Fatalf("read traffic scale integration test: %v", err)
	}
	text := string(body)
	for _, required := range []string{"//go:build integration", "600", "RecordPendingBatch", "used_bytes"} {
		if !strings.Contains(text, required) {
			t.Errorf("traffic scale integration test missing %q", required)
		}
	}
}
