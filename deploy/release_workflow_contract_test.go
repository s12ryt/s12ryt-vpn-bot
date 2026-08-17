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

func TestSingBoxDockerfileUsesOfficialLinuxBuildRequirements(t *testing.T) {
	body, err := os.ReadFile("../Dockerfile.singbox")
	if err != nil {
		t.Fatalf("read sing-box Dockerfile: %v", err)
	}
	text := string(body)
	for _, required := range []string{"release/DEFAULT_BUILD_TAGS", "release/LDFLAGS", "with_purego", "with_v2ray_api", "TARGETARCH", "CGO_ENABLED=0", "USER nonroot:nonroot"} {
		if !strings.Contains(text, required) {
			t.Errorf("sing-box Dockerfile missing %q", required)
		}
	}
}

func TestReleaseNotesRenderRealNewlines(t *testing.T) {
	body, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.Contains(line, "--notes") && strings.Contains(line, `\n`) {
			t.Fatalf("release --notes must not embed literal backslash-n (it renders as text): %s", strings.TrimSpace(line))
		}
	}
	if !strings.Contains(string(body), "printf 'Source commit: ") {
		t.Fatal("release notes must be assembled with printf so newlines render correctly")
	}
}

func TestReleaseWorkflowScansBeforePublishingImages(t *testing.T) {
	body, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	workflow := string(body)
	firstScan := strings.Index(workflow, "trivy")
	firstPush := strings.Index(workflow, "--push")
	if firstScan == -1 {
		t.Fatal("release workflow must run Trivy")
	}
	if firstPush == -1 {
		t.Fatal("release workflow must publish images")
	}
	if firstPush < firstScan {
		t.Fatal("images must be scanned locally before the first --push; a gate that publishes first leaks vulnerable images to the public registry")
	}
	if strings.Contains(workflow, "Scan published image") {
		t.Fatal("push-then-scan step must be removed; scanning must gate publication")
	}
	if !strings.Contains(workflow, "--load") {
		t.Fatal("workflow must load a locally built image for the pre-publication scan")
	}
}

func TestPackageCleanupWorkflowIsConstrained(t *testing.T) {
	body, err := os.ReadFile("../.github/workflows/package-cleanup.yml")
	if err != nil {
		t.Fatalf("read package cleanup workflow: %v", err)
	}
	workflow := string(body)
	for _, required := range []string{
		"workflow_dispatch:", "packages: write", "s12ryt-sing-box",
		"/user/packages/container/s12ryt-sing-box/versions",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("package cleanup workflow missing %q", required)
		}
	}
	if strings.Contains(workflow, "delete:packages") {
		t.Fatal("workflow must use GITHUB_TOKEN, not a PAT secret")
	}
	if regexp.MustCompile(`inputs\.[A-Za-z_]+`).FindAllString(workflow, -1) == nil {
		t.Fatal("workflow must accept a constrained tag input")
	}
	if !strings.Contains(workflow, "delete_package") {
		t.Fatal("workflow must expose an explicit full-package deletion mode for the last-tagged-version case")
	}
	if !strings.Contains(workflow, `gh api -X DELETE /user/packages/container/s12ryt-sing-box`) {
		t.Fatal("full-package mode must call the documented package deletion endpoint")
	}
	for _, forbidden := range []string{"inputs.package", "inputs.owner"} {
		if strings.Contains(workflow, forbidden) {
			t.Errorf("workflow input %q would allow arbitrary package targeting", forbidden)
		}
	}
	action := regexp.MustCompile(`uses: [A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+@([0-9a-f]{40})`)
	if uses := strings.Count(workflow, "uses: "); uses > 0 && len(action.FindAllStringSubmatch(workflow, -1)) != uses {
		t.Fatal("all cleanup actions must be pinned to full commit SHAs")
	}
}
