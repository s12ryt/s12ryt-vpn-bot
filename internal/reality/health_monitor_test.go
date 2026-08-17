package reality

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestHealthMonitorNotifiesOnlyOnPersistedTransitions(t *testing.T) {
	now := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	provider := &healthTargetProvider{target: "www.example.com"}
	prober := &healthProber{outcomes: []error{errors.New("dial failed"), errors.New("dial failed"), nil}}
	recorder := &healthRecorder{transitions: []HealthTransition{HealthTransitionFailed, HealthTransitionNone, HealthTransitionRecovered}}
	notifier := &healthNotifier{}
	monitor, err := NewHealthMonitor(provider, prober, recorder, notifier, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewHealthMonitor() error = %v", err)
	}

	for attempt := 0; attempt < 3; attempt++ {
		if err := monitor.Check(context.Background()); err != nil {
			t.Fatalf("Check(%d) error = %v", attempt, err)
		}
	}

	if got, want := recorder.observations, []healthObservation{
		{target: "www.example.com", healthy: false, at: now},
		{target: "www.example.com", healthy: false, at: now},
		{target: "www.example.com", healthy: true, at: now},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("observations = %#v, want %#v", got, want)
	}
	if notifier.failures != 1 || notifier.recoveries != 1 {
		t.Fatalf("notifications = failures:%d recoveries:%d", notifier.failures, notifier.recoveries)
	}
	if recorder.acknowledgements != 2 {
		t.Fatalf("acknowledgements = %d, want 2", recorder.acknowledgements)
	}
}

func TestHealthMonitorSkipsUnconfiguredTarget(t *testing.T) {
	provider := &healthTargetProvider{err: ErrRealityTargetNotConfigured}
	prober := &healthProber{}
	recorder := &healthRecorder{}
	monitor, err := NewHealthMonitor(provider, prober, recorder, &healthNotifier{}, time.Now)
	if err != nil {
		t.Fatalf("NewHealthMonitor() error = %v", err)
	}
	if err := monitor.Check(context.Background()); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if prober.calls != 0 || len(recorder.observations) != 0 {
		t.Fatalf("unconfigured target touched dependencies: probes=%d observations=%d", prober.calls, len(recorder.observations))
	}
}

func TestHealthMonitorRunUsesHourlyCadenceAndSurvivesCheckFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	provider := &healthTargetProvider{target: "www.example.com"}
	prober := &healthProber{}
	recorder := &healthRecorder{err: errors.New("database unavailable")}
	waits := make([]time.Duration, 0, 2)
	wait := func(ctx context.Context, duration time.Duration) error {
		waits = append(waits, duration)
		if len(waits) == 2 {
			cancel()
		}
		return nil
	}
	monitor, err := NewHealthMonitor(provider, prober, recorder, &healthNotifier{}, time.Now)
	if err != nil {
		t.Fatalf("NewHealthMonitor() error = %v", err)
	}
	if err := monitor.Run(ctx, wait); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := waits, []time.Duration{time.Hour, time.Hour}; !reflect.DeepEqual(got, want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}
	if prober.calls != 2 {
		t.Fatalf("prober calls = %d, want 2", prober.calls)
	}
}

func TestNewHealthMonitorRejectsMissingDependencies(t *testing.T) {
	if _, err := NewHealthMonitor(nil, &healthProber{}, &healthRecorder{}, &healthNotifier{}, time.Now); err == nil {
		t.Fatal("NewHealthMonitor() accepted nil provider")
	}
}

type healthTargetProvider struct {
	target string
	err    error
}

func (provider *healthTargetProvider) CurrentRealityTarget(context.Context) (string, error) {
	return provider.target, provider.err
}

type healthProber struct {
	outcomes []error
	calls    int
}

func (prober *healthProber) Probe(_ context.Context, domain string) (Target, error) {
	index := prober.calls
	prober.calls++
	if index < len(prober.outcomes) && prober.outcomes[index] != nil {
		return Target{}, prober.outcomes[index]
	}
	return Target{Domain: domain, TLS13: true, Latency: time.Millisecond}, nil
}

type healthObservation struct {
	target  string
	healthy bool
	at      time.Time
}

type healthRecorder struct {
	transitions      []HealthTransition
	observations     []healthObservation
	err              error
	acknowledgements int
}

func (recorder *healthRecorder) AcknowledgeRealityHealthNotification(context.Context, string, HealthTransition, time.Time) error {
	recorder.acknowledgements++
	return nil
}

func (recorder *healthRecorder) RecordRealityHealth(_ context.Context, target string, healthy bool, at time.Time) (HealthTransition, error) {
	recorder.observations = append(recorder.observations, healthObservation{target: target, healthy: healthy, at: at})
	if recorder.err != nil {
		return "", recorder.err
	}
	index := len(recorder.observations) - 1
	if index < len(recorder.transitions) {
		return recorder.transitions[index], nil
	}
	return HealthTransitionNone, nil
}

type healthNotifier struct {
	failures   int
	recoveries int
}

func (notifier *healthNotifier) NotifyRealityFailure(context.Context, string) error {
	notifier.failures++
	return nil
}

func (notifier *healthNotifier) NotifyRealityRecovery(context.Context, string) error {
	notifier.recoveries++
	return nil
}
