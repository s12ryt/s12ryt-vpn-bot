package reality

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestEmbeddedDatasetVerifiesPinnedChecksum(t *testing.T) {
	dataset, err := NewEmbeddedDataset()
	if err != nil {
		t.Fatalf("NewEmbeddedDataset() error = %v", err)
	}
	domains, err := dataset.Domains(context.Background())
	if err != nil {
		t.Fatalf("Domains() error = %v", err)
	}
	if len(domains) < 20 {
		t.Fatalf("dataset has only %d domains, want a meaningful candidate pool", len(domains))
	}
	for _, domain := range domains {
		if !validDomain(strings.ToLower(domain)) {
			t.Fatalf("dataset contains invalid domain %q", domain)
		}
	}
	if got := hex.EncodeToString(datasetSHA256(rawDataset(t))); got != datasetChecksum() {
		t.Fatalf("dataset checksum = %s, want pinned %s", got, datasetChecksum())
	}
}

func TestEmbeddedDatasetRejectsChecksumMismatch(t *testing.T) {
	if err := verifyDatasetChecksum([]byte("tampered"), datasetChecksum()); err == nil {
		t.Fatal("tampered dataset must be rejected")
	}
	if err := verifyDatasetChecksum(rawDataset(t), "deadbeef"); err == nil {
		t.Fatal("mismatched pinned checksum must be rejected")
	}
	if err := verifyDatasetChecksum(rawDataset(t), datasetChecksum()); err != nil {
		t.Fatalf("valid dataset rejected: %v", err)
	}
	if err := verifyDatasetChecksum(nil, datasetChecksum()); !errors.Is(err, errDatasetEmpty) {
		t.Fatalf("empty dataset error = %v", err)
	}
}

func rawDataset(t *testing.T) []byte {
	t.Helper()
	contents, err := datasetFiles.ReadFile(datasetFile)
	if err != nil {
		t.Fatalf("read dataset: %v", err)
	}
	return contents
}
