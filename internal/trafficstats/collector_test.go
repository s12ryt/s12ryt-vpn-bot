package trafficstats

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestCollectorQueriesAndAggregatesUserTraffic(t *testing.T) {
	rpc := &fakeRPC{response: []Stat{
		{Name: "user>>>2002>>>traffic>>>downlink", Value: 7},
		{Name: "user>>>1001>>>traffic>>>uplink", Value: 11},
		{Name: "user>>>1001>>>traffic>>>downlink", Value: 13},
	}}
	collector, err := NewCollector(rpc)
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	samples, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	wantRequest := QueryRequest{
		Patterns: []string{`^user>>>[1-9][0-9]*>>>traffic>>>(uplink|downlink)$`},
		Reset:    true,
		Regexp:   true,
	}
	if !reflect.DeepEqual(rpc.request, wantRequest) {
		t.Fatalf("QueryStats() request = %#v, want %#v", rpc.request, wantRequest)
	}
	want := []Sample{
		{TelegramID: 1001, Uplink: 11, Downlink: 13},
		{TelegramID: 2002, Downlink: 7},
	}
	if !reflect.DeepEqual(samples, want) {
		t.Fatalf("Collect() = %#v, want %#v", samples, want)
	}
}

func TestCollectorRejectsInvalidBatchWithoutPartialSamples(t *testing.T) {
	tests := []struct {
		name  string
		stats []Stat
	}{
		{name: "malformed name", stats: []Stat{{Name: "user>>>1001>>>traffic>>>other", Value: 1}}},
		{name: "zero telegram id", stats: []Stat{{Name: "user>>>0>>>traffic>>>uplink", Value: 1}}},
		{name: "negative value", stats: []Stat{{Name: "user>>>1001>>>traffic>>>uplink", Value: -1}}},
		{name: "duplicate counter", stats: []Stat{
			{Name: "user>>>1001>>>traffic>>>uplink", Value: 1},
			{Name: "user>>>1001>>>traffic>>>uplink", Value: 2},
		}},
		{name: "aggregate overflow", stats: []Stat{
			{Name: "user>>>1001>>>traffic>>>uplink", Value: math.MaxInt64},
			{Name: "user>>>1001>>>traffic>>>downlink", Value: 1},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			collector, err := NewCollector(&fakeRPC{response: test.stats})
			if err != nil {
				t.Fatalf("NewCollector() error = %v", err)
			}
			samples, err := collector.Collect(context.Background())
			if err == nil {
				t.Fatal("Collect() error = nil, want error")
			}
			if samples != nil {
				t.Fatalf("Collect() samples = %#v, want nil", samples)
			}
		})
	}
}

func TestCollectorPreservesRPCFailure(t *testing.T) {
	wantErr := errors.New("rpc unavailable")
	collector, err := NewCollector(&fakeRPC{err: wantErr})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	if samples, err := collector.Collect(context.Background()); !errors.Is(err, wantErr) || samples != nil {
		t.Fatalf("Collect() = (%#v, %v), want nil wrapping %v", samples, err, wantErr)
	}
}

func TestCollectorRejectsNilRPC(t *testing.T) {
	if _, err := NewCollector(nil); err == nil {
		t.Fatal("NewCollector(nil) error = nil, want error")
	}
}

type fakeRPC struct {
	request  QueryRequest
	response []Stat
	err      error
}

func (rpc *fakeRPC) QueryStats(_ context.Context, request QueryRequest) ([]Stat, error) {
	rpc.request = request
	if rpc.err != nil {
		return nil, rpc.err
	}
	return append([]Stat(nil), rpc.response...), nil
}
