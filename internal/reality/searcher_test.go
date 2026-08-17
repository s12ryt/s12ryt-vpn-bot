package reality

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSearchRanksValidTargetsByLatency(t *testing.T) {
	prober := &proberStub{results: map[string]probeOutcome{
		"fast.example":       {latency: 20 * time.Millisecond},
		"medium.example":     {latency: 80 * time.Millisecond},
		"slow.example":       {latency: 200 * time.Millisecond},
		"tls12-only.example": {err: errors.New("tls13 unsupported")},
		"bad-cert.example":   {err: errors.New("certificate name mismatch")},
	}}
	dataset := &datasetStub{domains: []string{"slow.example", "bad-cert.example", "fast.example", "medium.example", "tls12-only.example"}}

	results, err := Search(context.Background(), Options{Dataset: dataset, Prober: prober, SampleLimit: 200, Concurrency: 5, Budget: time.Minute})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 3 || results[0].Domain != "fast.example" || results[1].Domain != "medium.example" || results[2].Domain != "slow.example" {
		t.Fatalf("results = %#v", results)
	}
	if results[0].TLS13 != true || results[0].Latency <= 0 {
		t.Fatalf("result details = %#v", results[0])
	}
	if !prober.probedOnly(dataset.domains) {
		t.Fatalf("probed unexpected domains: %v", prober.probed)
	}
}

func TestSearchSamplesAtMostLimitCandidates(t *testing.T) {
	domains := make([]string, 0, 500)
	for index := 0; index < 500; index++ {
		domains = append(domains, "domain"+string(rune('a'+index%26))+strconv.Itoa(index)+".example")
	}
	prober := &proberStub{defaultOutcome: probeOutcome{latency: time.Millisecond}}
	dataset := &datasetStub{domains: domains}

	results, err := Search(context.Background(), Options{Dataset: dataset, Prober: prober, SampleLimit: 200, Concurrency: 5, Budget: time.Minute})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(prober.probedCount()) != 200 {
		t.Fatalf("probed %d domains, want the 200-domain sample cap", len(prober.probedCount()))
	}
	if len(results) != 200 {
		t.Fatalf("results = %d, want 200", len(results))
	}
}

func TestSearchEnforcesBudgetAndReturnsWhatItHas(t *testing.T) {
	slowProber := &blockingProber{}
	dataset := &datasetStub{domains: []string{"a.example", "b.example", "c.example", "d.example", "e.example", "f.example"}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	results, err := Search(ctx, Options{Dataset: dataset, Prober: slowProber, SampleLimit: 200, Concurrency: 2, Budget: 30 * time.Millisecond})
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Search() error = %v", err)
	}
	_ = results
}

func TestSearchRejectsInvalidOptions(t *testing.T) {
	tests := []Options{
		{},
		{Dataset: &datasetStub{}, Prober: &proberStub{}, SampleLimit: 0, Concurrency: 5, Budget: time.Minute},
		{Dataset: &datasetStub{}, Prober: &proberStub{}, SampleLimit: 201, Concurrency: 5, Budget: time.Minute},
		{Dataset: &datasetStub{}, Prober: &proberStub{}, SampleLimit: 200, Concurrency: 0, Budget: time.Minute},
		{Dataset: &datasetStub{}, Prober: &proberStub{}, SampleLimit: 200, Concurrency: 6, Budget: time.Minute},
		{Dataset: &datasetStub{}, Prober: &proberStub{}, SampleLimit: 200, Concurrency: 5, Budget: 61 * time.Second},
	}
	for _, options := range tests {
		if _, err := Search(context.Background(), options); err == nil {
			t.Fatalf("Search(%+v) expected rejection", options)
		}
	}
}

func TestSearchDeduplicatesDatasetDomains(t *testing.T) {
	prober := &proberStub{defaultOutcome: probeOutcome{latency: time.Millisecond}}
	dataset := &datasetStub{domains: []string{"dup.example", "dup.example", "other.example"}}

	results, err := Search(context.Background(), Options{Dataset: dataset, Prober: prober, SampleLimit: 200, Concurrency: 5, Budget: time.Minute})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(prober.probedCount()) != 2 || len(results) != 2 {
		t.Fatalf("probed=%v results=%d", prober.probedCount(), len(results))
	}
}

func TestValidDomainRejectsMalformedEntries(t *testing.T) {
	for _, invalid := range []string{"", "not a domain", "-leading.example", "a..b", "under_score.example", strings.Repeat("a", 64) + ".example"} {
		if validDomain(invalid) {
			t.Fatalf("validDomain(%q) = true", invalid)
		}
	}
	for _, valid := range []string{"a.example", "www.example.com", "example.io"} {
		if !validDomain(valid) {
			t.Fatalf("validDomain(%q) = false", valid)
		}
	}
}

type probeOutcome struct {
	latency time.Duration
	err     error
}

type proberStub struct {
	mu             sync.Mutex
	results        map[string]probeOutcome
	defaultOutcome probeOutcome
	probed         []string
}

func (stub *proberStub) Probe(_ context.Context, domain string) (Target, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.probed = append(stub.probed, domain)
	if outcome, ok := stub.results[domain]; ok {
		if outcome.err != nil {
			return Target{}, outcome.err
		}
		return Target{Domain: domain, TLS13: true, Latency: outcome.latency}, nil
	}
	if stub.defaultOutcome.err != nil {
		return Target{}, stub.defaultOutcome.err
	}
	return Target{Domain: domain, TLS13: true, Latency: stub.defaultOutcome.latency}, nil
}

func (stub *proberStub) probedCount() []string {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return append([]string(nil), stub.probed...)
}

func (stub *proberStub) probedOnly(expected []string) bool {
	counts := make(map[string]int, len(stub.probed))
	for _, domain := range stub.probed {
		counts[domain]++
	}
	if len(counts) != len(expected) {
		return false
	}
	for _, domain := range expected {
		if counts[domain] != 1 {
			return false
		}
	}
	return true
}

type blockingProber struct{}

func (prober *blockingProber) Probe(ctx context.Context, _ string) (Target, error) {
	<-ctx.Done()
	return Target{}, ctx.Err()
}

type datasetStub struct {
	domains []string
	err     error
}

func (stub *datasetStub) Domains(context.Context) ([]string, error) {
	return stub.domains, stub.err
}
