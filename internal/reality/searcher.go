// Package reality searches for candidate REALITY masquerade targets from a
// pinned public domain dataset. It only performs DNS resolution, TCP 443
// connections, TLS 1.3 handshakes, certificate-name checks and latency
// measurement; it never scans other ports or IP ranges, and never applies a
// target automatically — owners confirm the final choice.
package reality

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"regexp"
	"sort"
	"sync"
	"time"
)

// Target is a probe-verified REALITY candidate.
type Target struct {
	Domain  string        `json:"domain"`
	TLS13   bool          `json:"tls13"`
	Latency time.Duration `json:"-"`
}

// MarshalJSON emits the probe latency in milliseconds so the JSON field name
// matches the unit; time.Duration would otherwise marshal as nanoseconds.
func (target Target) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Domain    string `json:"domain"`
		TLS13     bool   `json:"tls13"`
		LatencyMS int64  `json:"latency_ms"`
	}{Domain: target.Domain, TLS13: target.TLS13, LatencyMS: target.Latency.Milliseconds()})
}

// UnmarshalJSON accepts the millisecond representation and restores the
// duration, keeping the wire format symmetric.
func (target *Target) UnmarshalJSON(data []byte) error {
	var wire struct {
		Domain    string `json:"domain"`
		TLS13     bool   `json:"tls13"`
		LatencyMS int64  `json:"latency_ms"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	target.Domain = wire.Domain
	target.TLS13 = wire.TLS13
	target.Latency = time.Duration(wire.LatencyMS) * time.Millisecond
	return nil
}

// Dataset provides candidate domains from a pinned, checksum-verified source.
type Dataset interface {
	Domains(context.Context) ([]string, error)
}

// Prober measures one domain. The production implementation resolves DNS,
// dials TCP 443, requires a TLS 1.3 handshake with a matching certificate and
// reports the total latency.
type Prober interface {
	Probe(context.Context, string) (Target, error)
}

// Options constrains a search. SampleLimit is capped at 200 domains,
// Concurrency at 5 workers and Budget at 60 seconds per the deployment
// contract.
type Options struct {
	Dataset     Dataset
	Prober      Prober
	SampleLimit int
	Concurrency int
	Budget      time.Duration
	// Random seeds candidate sampling; nil uses math/rand.
	Random *rand.Rand
}

const (
	maxSampleLimit = 200
	maxConcurrency = 5
	maxBudget      = 60 * time.Second
)

var domainPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// Search samples at most SampleLimit unique valid domains from the dataset
// and probes them with at most Concurrency parallel workers inside Budget.
// Results are ranked by ascending latency with a deterministic domain
// tiebreak; probing failures are skipped, not errors.
func Search(ctx context.Context, options Options) ([]Target, error) {
	if options.Dataset == nil || options.Prober == nil {
		return nil, errors.New("reality search requires a dataset and prober")
	}
	if options.SampleLimit < 1 || options.SampleLimit > maxSampleLimit {
		return nil, errors.New("reality search sample limit must be between 1 and 200")
	}
	if options.Concurrency < 1 || options.Concurrency > maxConcurrency {
		return nil, errors.New("reality search concurrency must be between 1 and 5")
	}
	if options.Budget <= 0 || options.Budget > maxBudget {
		return nil, errors.New("reality search budget must be between 1s and 60s")
	}
	domains, err := options.Dataset.Domains(ctx)
	if err != nil {
		return nil, err
	}
	unique := make([]string, 0, len(domains))
	seen := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		domain = normalizeDomain(domain)
		if !validDomain(domain) {
			continue
		}
		if _, duplicate := seen[domain]; duplicate {
			continue
		}
		seen[domain] = struct{}{}
		unique = append(unique, domain)
	}
	sampled := sampleDomains(unique, options.SampleLimit, options.Random)

	searchContext, cancel := context.WithTimeout(ctx, options.Budget)
	defer cancel()

	work := make(chan string)
	results := make(chan Target, len(sampled))
	var workers sync.WaitGroup
	for worker := 0; worker < options.Concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for domain := range work {
				target, probeErr := options.Prober.Probe(searchContext, domain)
				if probeErr != nil || !target.TLS13 || target.Domain != domain {
					continue
				}
				if target.Latency <= 0 {
					continue
				}
				results <- target
			}
		}()
	}
	for _, domain := range sampled {
		select {
		case work <- domain:
		case <-searchContext.Done():
			close(work)
			workers.Wait()
			close(results)
			return rank(selectConfirmed(searchContext, results)), searchContext.Err()
		}
	}
	close(work)
	workers.Wait()
	close(results)
	return rank(drain(results)), nil
}

func selectConfirmed(ctx context.Context, results <-chan Target) []Target {
	var collected []Target
	for {
		select {
		case target := <-results:
			collected = append(collected, target)
		case <-ctx.Done():
			return collected
		}
	}
}

func drain(results <-chan Target) []Target {
	var collected []Target
	for target := range results {
		collected = append(collected, target)
	}
	return collected
}

func rank(targets []Target) []Target {
	sort.Slice(targets, func(left, right int) bool {
		if targets[left].Latency != targets[right].Latency {
			return targets[left].Latency < targets[right].Latency
		}
		return targets[left].Domain < targets[right].Domain
	})
	return targets
}

func sampleDomains(domains []string, limit int, random *rand.Rand) []string {
	if len(domains) <= limit {
		return domains
	}
	if random == nil {
		random = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	sampled := make([]string, limit)
	copy(sampled, domains[:limit])
	for index := limit; index < len(domains); index++ {
		position := random.Intn(index + 1)
		if position < limit {
			sampled[position] = domains[index]
		}
	}
	return sampled
}

func normalizeDomain(domain string) string {
	for len(domain) > 0 && domain[0] == '.' {
		domain = domain[1:]
	}
	return domain
}

func validDomain(domain string) bool {
	return len(domain) >= 4 && len(domain) <= 253 && domainPattern.MatchString(domain)
}
