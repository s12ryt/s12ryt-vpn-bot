package reality

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type serviceDataset struct {
	mu      sync.Mutex
	domains []string
	err     error
	calls   int
}

func (d *serviceDataset) Domains(context.Context) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if d.err != nil {
		return nil, d.err
	}
	return d.domains, nil
}

type releaseProber struct {
	release chan struct{}
}

func (prober *releaseProber) Probe(ctx context.Context, domain string) (Target, error) {
	select {
	case <-prober.release:
		return Target{Domain: domain, TLS13: true, Latency: 10 * time.Millisecond}, nil
	case <-ctx.Done():
		return Target{}, ctx.Err()
	}
}

func TestServiceStartRunsSearchInBackground(t *testing.T) {
	dataset := &serviceDataset{domains: []string{"www.example.com", "api.example.org"}}
	service := NewService(Options{
		Dataset:     dataset,
		Prober:      &proberStub{defaultOutcome: probeOutcome{latency: 10 * time.Millisecond}},
		SampleLimit: 2,
		Concurrency: 1,
		Budget:      time.Second,
	})
	if service == nil {
		t.Fatal("NewService returned nil")
	}
	if snapshot := service.Snapshot(); snapshot.Status != SearchStatusIdle {
		t.Fatalf("initial status = %q, want %q", snapshot.Status, SearchStatusIdle)
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for service.Snapshot().Status == SearchStatusRunning {
		if time.Now().After(deadline) {
			t.Fatal("search did not finish in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
	snapshot := service.Snapshot()
	if snapshot.Status != SearchStatusCompleted {
		t.Fatalf("status = %q, want %q", snapshot.Status, SearchStatusCompleted)
	}
	if snapshot.Error != "" {
		t.Fatalf("Error = %q, want empty", snapshot.Error)
	}
	if len(snapshot.Targets) == 0 {
		t.Fatal("Targets empty after completed search")
	}
	if snapshot.StartedAt.IsZero() {
		t.Fatal("StartedAt is zero")
	}
}

func TestServiceStartRejectsConcurrentRuns(t *testing.T) {
	release := make(chan struct{})
	dataset := &serviceDataset{domains: []string{"www.example.com"}}
	prober := &releaseProber{release: release}
	service := NewService(Options{
		Dataset:     dataset,
		Prober:      prober,
		SampleLimit: 1,
		Concurrency: 1,
		Budget:      5 * time.Second,
	})
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	if snapshot := service.Snapshot(); snapshot.Status != SearchStatusRunning {
		t.Fatalf("status after start = %q, want %q", snapshot.Status, SearchStatusRunning)
	}
	if err := service.Start(context.Background()); !errors.Is(err, ErrSearchRunning) {
		t.Fatalf("second Start() error = %v, want ErrSearchRunning", err)
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for service.Snapshot().Status == SearchStatusRunning {
		if time.Now().After(deadline) {
			t.Fatal("search did not finish in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start() after completion error = %v", err)
	}
}

func TestServiceRecordsFailure(t *testing.T) {
	dataset := &serviceDataset{err: errors.New("dataset unavailable")}
	service := NewService(Options{
		Dataset:     dataset,
		Prober:      &proberStub{},
		SampleLimit: 1,
		Concurrency: 1,
		Budget:      time.Second,
	})
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for service.Snapshot().Status == SearchStatusRunning {
		if time.Now().After(deadline) {
			t.Fatal("search did not finish in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
	snapshot := service.Snapshot()
	if snapshot.Status != SearchStatusFailed {
		t.Fatalf("status = %q, want %q", snapshot.Status, SearchStatusFailed)
	}
	if snapshot.Error == "" {
		t.Fatal("Error empty on failed search")
	}
	if snapshot.Targets != nil {
		t.Fatalf("Targets = %v, want nil on failure", snapshot.Targets)
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start() after failure error = %v", err)
	}
}

func TestNewServiceRejectsInvalidOptions(t *testing.T) {
	valid := Options{
		Dataset:     &serviceDataset{},
		Prober:      &proberStub{},
		SampleLimit: 10,
		Concurrency: 2,
		Budget:      10 * time.Second,
	}
	cases := []struct {
		name   string
		mutate func(Options) Options
	}{
		{"missing dataset", func(o Options) Options { o.Dataset = nil; return o }},
		{"missing prober", func(o Options) Options { o.Prober = nil; return o }},
		{"zero sample limit", func(o Options) Options { o.SampleLimit = 0; return o }},
		{"oversized sample limit", func(o Options) Options { o.SampleLimit = maxSampleLimit + 1; return o }},
		{"zero concurrency", func(o Options) Options { o.Concurrency = 0; return o }},
		{"oversized concurrency", func(o Options) Options { o.Concurrency = maxConcurrency + 1; return o }},
		{"zero budget", func(o Options) Options { o.Budget = 0; return o }},
		{"oversized budget", func(o Options) Options { o.Budget = maxBudget + time.Second; return o }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if service := NewService(testCase.mutate(valid)); service != nil {
				t.Fatalf("NewService() = %v, want nil", service)
			}
		})
	}
}

func TestServiceStartRejectsNilContext(t *testing.T) {
	service := NewService(Options{
		Dataset:     &serviceDataset{domains: []string{"www.example.com"}},
		Prober:      &proberStub{},
		SampleLimit: 1,
		Concurrency: 1,
		Budget:      time.Second,
	})
	if err := service.Start(nil); err == nil {
		t.Fatal("Start(nil) error = nil, want error")
	}
	if snapshot := service.Snapshot(); snapshot.Status != SearchStatusIdle {
		t.Fatalf("status after rejected start = %q, want %q", snapshot.Status, SearchStatusIdle)
	}
}

func TestTargetMarshalsLatencyInMilliseconds(t *testing.T) {
	encoded, err := json.Marshal(Target{Domain: "www.example.com", TLS13: true, Latency: 42 * time.Millisecond})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !strings.Contains(string(encoded), `"latency_ms":42}`) {
		t.Fatalf("encoded = %s, want latency_ms in milliseconds", encoded)
	}
	var decoded Target
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded.Latency != 42*time.Millisecond {
		t.Fatalf("round-trip latency = %v, want 42ms", decoded.Latency)
	}
}
