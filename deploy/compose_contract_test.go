package deploy

import (
	"os"
	"strings"
	"testing"
)

func TestComposePreservesCoreControlSecurityBoundary(t *testing.T) {
	body, err := os.ReadFile("../compose.yaml")
	if err != nil {
		t.Fatalf("read compose: %v", err)
	}
	compose := string(body)
	for _, required := range []string{
		"network_mode: host", "127.0.0.1:${POSTGRES_PORT:-5432}:5432",
		"${SINGBOX_IMAGE:?set SINGBOX_IMAGE to an immutable version or digest}",
		"/var/run/docker.sock:/var/run/docker.sock", "core-control:/run/s12ryt",
		"read_only: true", "no-new-privileges:true",
	} {
		if !strings.Contains(compose, required) {
			t.Errorf("compose missing %q", required)
		}
	}
	appSection := between(compose, "  app:\n", "  core-controller:\n")
	if strings.Contains(appSection, "/var/run/docker.sock") {
		t.Fatal("app must not mount Docker socket")
	}
	controllerSection := between(compose, "  core-controller:\n", "  sing-box:\n")
	if strings.Count(controllerSection, "/var/run/docker.sock") != 2 {
		t.Fatal("controller must be the only Docker socket consumer")
	}
}

func TestContainerBuildsAreReproducibleAndUnprivileged(t *testing.T) {
	for _, file := range []string{"../Dockerfile", "../Dockerfile.controller"} {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		text := string(body)
		for _, required := range []string{"CGO_ENABLED=0", "-trimpath", "USER nonroot:nonroot"} {
			if !strings.Contains(text, required) {
				t.Errorf("%s missing %q", file, required)
			}
		}
	}
}

func between(value, start, end string) string {
	startIndex := strings.Index(value, start)
	if startIndex < 0 {
		return ""
	}
	remaining := value[startIndex+len(start):]
	endIndex := strings.Index(remaining, end)
	if endIndex < 0 {
		return remaining
	}
	return remaining[:endIndex]
}
