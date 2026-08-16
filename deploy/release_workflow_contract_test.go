package deploy

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestReleaseWorkflowBuildsTraceableMultiArchitectureImages(t *testing.T) {
	body, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	workflow := string(body)
	for _, required := range []string{
		"prerelease == false", "draft == false", "release/DEFAULT_BUILD_TAGS",
		"with_v2ray_api", "linux/amd64,linux/arm64", "--sbom=true",
		"--provenance=mode=max", "source_sha256", "trivy@v0.66.0",
		"ghcr.io/${{ github.repository_owner }}/s12ryt-sing-box",
		"ghcr.io/${{ github.repository_owner }}/s12ryt-vpn-bot",
		"ghcr.io/${{ github.repository_owner }}/s12ryt-vpn-core-controller",
		"attestations: write", "packages: write", "id-token: write",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("release workflow missing %q", required)
		}
	}
	if strings.Contains(workflow, ":latest") {
		t.Fatal("release workflow must not publish mutable latest tags")
	}
	action := regexp.MustCompile(`uses: [A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+@([0-9a-f]{40})`)
	if uses := strings.Count(workflow, "uses: "); uses == 0 || len(action.FindAllStringSubmatch(workflow, -1)) != uses {
		t.Fatal("all release actions must be pinned to full commit SHAs")
	}
}

func TestSingBoxDockerfileAddsOnlyRequiredBuildTag(t *testing.T) {
	body, err := os.ReadFile("../Dockerfile.singbox")
	if err != nil {
		t.Fatalf("read sing-box Dockerfile: %v", err)
	}
	text := string(body)
	for _, required := range []string{"release/DEFAULT_BUILD_TAGS", "with_v2ray_api", "TARGETARCH", "CGO_ENABLED=0", "USER nonroot:nonroot"} {
		if !strings.Contains(text, required) {
			t.Errorf("sing-box Dockerfile missing %q", required)
		}
	}
}
