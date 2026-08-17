package reality

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
)

//go:embed dataset-top-domains.txt
var datasetFiles embed.FS

const datasetFile = "dataset-top-domains.txt"

// datasetPinnedSHA256 pins the shipped candidate list; changing the file
// requires updating this constant in the same commit.
const datasetPinnedSHA256 = "e0545a8a80df35c8efe5b81448da034cfc037ad076431c10533fe4a200429009"

var (
	errDatasetEmpty = errors.New("reality dataset is empty")
	datasetOnce     sync.Once
	datasetCached   []string
	datasetLoadErr  error
)

// EmbeddedDataset serves the pinned public top-domain list shipped with the
// binary. The file carries a pinned SHA-256 so the candidate pool cannot be
// swapped without a code change; probing still only ever touches port 443 of
// the listed domains.
type EmbeddedDataset struct{}

func NewEmbeddedDataset() (*EmbeddedDataset, error) {
	if _, err := loadDataset(); err != nil {
		return nil, err
	}
	return &EmbeddedDataset{}, nil
}

func (dataset *EmbeddedDataset) Domains(context.Context) ([]string, error) {
	domains, err := loadDataset()
	if err != nil {
		return nil, err
	}
	return append([]string(nil), domains...), nil
}

func datasetChecksum() string { return datasetPinnedSHA256 }

func datasetSHA256(contents []byte) []byte {
	digest := sha256.Sum256(contents)
	return digest[:]
}

func verifyDatasetChecksum(contents []byte, pinned string) error {
	if len(contents) == 0 {
		return errDatasetEmpty
	}
	if hex.EncodeToString(datasetSHA256(contents)) != pinned {
		return errors.New("reality dataset checksum mismatch")
	}
	return nil
}

func loadDataset() ([]string, error) {
	datasetOnce.Do(func() {
		contents, err := datasetFiles.ReadFile(datasetFile)
		if err != nil {
			datasetLoadErr = err
			return
		}
		if err := verifyDatasetChecksum(contents, datasetPinnedSHA256); err != nil {
			datasetLoadErr = err
			return
		}
		var domains []string
		for _, line := range strings.Split(string(contents), "\n") {
			line = strings.ToLower(strings.TrimSpace(line))
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			domains = append(domains, line)
		}
		if len(domains) == 0 {
			datasetLoadErr = errDatasetEmpty
			return
		}
		datasetCached = domains
	})
	return datasetCached, datasetLoadErr
}
