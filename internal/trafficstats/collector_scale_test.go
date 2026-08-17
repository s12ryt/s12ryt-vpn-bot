package trafficstats

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// TestCollectorAggregatesSixHundredUsersConcurrently verifies the production
// ceiling with the same per-user keys emitted by sing-box. Each counter is the
// sum expected from four protocols on both address families, while parallel
// collection exercises the shared Collector under the CI race detector.
func TestCollectorAggregatesSixHundredUsersConcurrently(t *testing.T) {
	stats := make([]Stat, 0, 1200)
	for id := int64(1); id <= 600; id++ {
		stats = append(stats,
			Stat{Name: fmt.Sprintf("user>>>%d>>>traffic>>>uplink", id), Value: id * 8},
			Stat{Name: fmt.Sprintf("user>>>%d>>>traffic>>>downlink", id), Value: id * 16},
		)
	}
	rpc := &concurrentScaleRPC{stats: stats}
	collector, err := NewCollector(rpc)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	const parallelIngestions = 16
	var wait sync.WaitGroup
	errors := make(chan error, parallelIngestions)
	for index := 0; index < parallelIngestions; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			samples, collectErr := collector.Collect(context.Background())
			if collectErr != nil {
				errors <- collectErr
				return
			}
			if len(samples) != 600 {
				errors <- fmt.Errorf("samples = %d, want 600", len(samples))
				return
			}
			for sampleIndex, sample := range samples {
				id := int64(sampleIndex + 1)
				if sample.TelegramID != id || sample.Uplink != id*8 || sample.Downlink != id*16 {
					errors <- fmt.Errorf("sample[%d] = %#v, want id=%d uplink=%d downlink=%d", sampleIndex, sample, id, id*8, id*16)
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errors)
	for collectErr := range errors {
		t.Error(collectErr)
	}
	if calls := rpc.Calls(); calls != parallelIngestions {
		t.Fatalf("QueryStats() calls = %d, want %d", calls, parallelIngestions)
	}
}

type concurrentScaleRPC struct {
	mu    sync.Mutex
	stats []Stat
	calls int
}

func (rpc *concurrentScaleRPC) QueryStats(_ context.Context, request QueryRequest) ([]Stat, error) {
	if !request.Reset || !request.Regexp || len(request.Patterns) != 1 || request.Patterns[0] != userTrafficPattern {
		return nil, fmt.Errorf("unexpected query request: %#v", request)
	}
	rpc.mu.Lock()
	rpc.calls++
	rpc.mu.Unlock()
	return append([]Stat(nil), rpc.stats...), nil
}

func (rpc *concurrentScaleRPC) Calls() int {
	rpc.mu.Lock()
	defer rpc.mu.Unlock()
	return rpc.calls
}
