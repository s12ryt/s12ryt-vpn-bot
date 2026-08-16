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

func readRepositoryFile(t *testing.T, name string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return contents
}
