package domain

import (
	"math"
	"testing"
	"time"
)

func TestQuotaWindowAggregatesProtocolsAndAddressFamilies(t *testing.T) {
	start := time.Date(2026, time.August, 16, 8, 0, 0, 0, time.UTC)
	window, err := NewQuotaWindow(start, 50_000_000_000, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("NewQuotaWindow() error = %v", err)
	}

	first, err := window.Record(TrafficSample{
		ObservedAt: start.Add(time.Minute),
		Protocol:   ProtocolVLESS,
		Family:     AddressFamilyIPv4,
		Uplink:     20_000_000_000,
	})
	if err != nil {
		t.Fatalf("Record(first) error = %v", err)
	}
	if first.Blocked {
		t.Fatal("first sample blocked before the shared quota was exhausted")
	}

	second, err := window.Record(TrafficSample{
		ObservedAt: start.Add(2 * time.Minute),
		Protocol:   ProtocolHysteria2,
		Family:     AddressFamilyIPv6,
		Downlink:   30_000_000_000,
	})
	if err != nil {
		t.Fatalf("Record(second) error = %v", err)
	}
	if second.UsedBytes != 50_000_000_000 {
		t.Fatalf("shared used bytes = %d, want 50000000000", second.UsedBytes)
	}
	if !second.Blocked {
		t.Fatal("second sample did not block at the shared quota boundary")
	}
}

func TestQuotaWindowStartsTheNextPeriodAndRestoresAccess(t *testing.T) {
	start := time.Date(2026, time.August, 16, 8, 0, 0, 0, time.UTC)
	period := 30 * 24 * time.Hour
	window, err := NewQuotaWindow(start, 100, period)
	if err != nil {
		t.Fatalf("NewQuotaWindow() error = %v", err)
	}

	if _, err := window.Record(TrafficSample{
		ObservedAt: start.Add(time.Minute),
		Protocol:   ProtocolTUIC,
		Family:     AddressFamilyIPv4,
		Uplink:     100,
	}); err != nil {
		t.Fatalf("Record(exhaustion) error = %v", err)
	}

	next, err := window.Record(TrafficSample{
		ObservedAt: start.Add(period).Add(time.Second),
		Protocol:   ProtocolAnyTLS,
		Family:     AddressFamilyIPv6,
		Downlink:   1,
	})
	if err != nil {
		t.Fatalf("Record(next period) error = %v", err)
	}
	if next.Blocked {
		t.Fatal("access remained blocked after the next period started")
	}
	if next.UsedBytes != 1 {
		t.Fatalf("next period used bytes = %d, want 1", next.UsedBytes)
	}
	if !next.PeriodStartedAt.Equal(start.Add(period)) {
		t.Fatalf("next period start = %s, want %s", next.PeriodStartedAt, start.Add(period))
	}
}

func TestQuotaWindowRejectsUnknownTrafficDimensions(t *testing.T) {
	start := time.Date(2026, time.August, 16, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		sample TrafficSample
	}{
		{
			name: "unknown protocol",
			sample: TrafficSample{
				ObservedAt: start,
				Protocol:   Protocol("unknown"),
				Family:     AddressFamilyIPv4,
				Uplink:     1,
			},
		},
		{
			name: "unknown address family",
			sample: TrafficSample{
				ObservedAt: start,
				Protocol:   ProtocolVLESS,
				Family:     AddressFamily("unknown"),
				Downlink:   1,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			window, err := NewQuotaWindow(start, 100, 30*24*time.Hour)
			if err != nil {
				t.Fatalf("NewQuotaWindow() error = %v", err)
			}

			if _, err := window.Record(test.sample); err == nil {
				t.Fatal("Record() error = nil, want invalid traffic dimension error")
			}
		})
	}
}

func TestQuotaWindowAdvanceRestoresBlockedUserAtPeriodBoundaryWithoutTraffic(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	period := 30 * 24 * time.Hour
	window, err := NewQuotaWindow(start, 100, period)
	if err != nil {
		t.Fatalf("NewQuotaWindow() error = %v", err)
	}
	blocked, err := window.Record(TrafficSample{
		ObservedAt: start.Add(time.Hour),
		Protocol:   ProtocolVLESS,
		Family:     AddressFamilyIPv6,
		Uplink:     100,
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if !blocked.Blocked {
		t.Fatal("Record() Blocked = false, want true")
	}

	beforeBoundary, err := window.Advance(start.Add(period - time.Nanosecond))
	if err != nil {
		t.Fatalf("Advance() before boundary error = %v", err)
	}
	if !beforeBoundary.Blocked || beforeBoundary.UsedBytes != 100 || !beforeBoundary.PeriodStartedAt.Equal(start) {
		t.Fatalf("Advance() before boundary = %#v, want unchanged blocked period", beforeBoundary)
	}

	atBoundary, err := window.Advance(start.Add(period))
	if err != nil {
		t.Fatalf("Advance() at boundary error = %v", err)
	}
	if atBoundary.Blocked || atBoundary.UsedBytes != 0 || !atBoundary.PeriodStartedAt.Equal(start.Add(period)) {
		t.Fatalf("Advance() at boundary = %#v, want fresh unblocked period", atBoundary)
	}
}

func TestQuotaWindowAdvanceRejectsEarlierTimeWithoutMutation(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	window, err := NewQuotaWindow(start, 100, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("NewQuotaWindow() error = %v", err)
	}
	before, err := window.Advance(start)
	if err != nil {
		t.Fatalf("Advance() setup error = %v", err)
	}

	if _, err := window.Advance(start.Add(-time.Nanosecond)); err == nil {
		t.Fatal("Advance() error = nil, want earlier timestamp error")
	}
	after, err := window.Advance(start)
	if err != nil {
		t.Fatalf("Advance() inspection error = %v", err)
	}
	if after != before {
		t.Fatalf("Advance() mutated snapshot from %#v to %#v", before, after)
	}
}

func TestQuotaWindowRejectsCounterOverflowWithoutMutatingUsage(t *testing.T) {
	start := time.Date(2026, time.August, 16, 8, 0, 0, 0, time.UTC)
	window, err := NewQuotaWindow(start, math.MaxInt64, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("NewQuotaWindow() error = %v", err)
	}

	if _, err := window.Record(TrafficSample{
		ObservedAt: start,
		Protocol:   ProtocolVLESS,
		Family:     AddressFamilyIPv4,
		Uplink:     math.MaxInt64,
		Downlink:   1,
	}); err == nil {
		t.Fatal("Record() error = nil, want traffic counter overflow error")
	}

	snapshot, err := window.Record(TrafficSample{
		ObservedAt: start.Add(time.Second),
		Protocol:   ProtocolVLESS,
		Family:     AddressFamilyIPv4,
		Uplink:     1,
	})
	if err != nil {
		t.Fatalf("Record(valid sample) error = %v", err)
	}
	if snapshot.UsedBytes != 1 {
		t.Fatalf("used bytes after rejected overflow = %d, want 1", snapshot.UsedBytes)
	}
}

func TestQuotaWindowAdjustLimitAppliesToCurrentUsageImmediately(t *testing.T) {
	start := time.Date(2026, time.August, 16, 8, 0, 0, 0, time.UTC)
	window, err := NewQuotaWindow(start, 100, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("NewQuotaWindow() error = %v", err)
	}
	if _, err := window.Record(TrafficSample{
		ObservedAt: start,
		Protocol:   ProtocolAnyTLS,
		Family:     AddressFamilyIPv6,
		Downlink:   60,
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	lowered, err := window.AdjustLimit(50)
	if err != nil {
		t.Fatalf("AdjustLimit(lower) error = %v", err)
	}
	if !lowered.Blocked || lowered.UsedBytes != 60 {
		t.Fatalf("lowered snapshot = %+v, want used 60 and blocked", lowered)
	}

	raised, err := window.AdjustLimit(70)
	if err != nil {
		t.Fatalf("AdjustLimit(raise) error = %v", err)
	}
	if raised.Blocked || raised.UsedBytes != 60 {
		t.Fatalf("raised snapshot = %+v, want used 60 and unblocked", raised)
	}
}

func TestRestoreQuotaWindowPreservesPersistedUsage(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	window, err := RestoreQuotaWindow(start, 100, 30*24*time.Hour, 100)
	if err != nil {
		t.Fatalf("RestoreQuotaWindow() error = %v", err)
	}
	snapshot, err := window.Advance(start)
	if err != nil {
		t.Fatalf("Advance() error = %v", err)
	}
	if snapshot.UsedBytes != 100 || !snapshot.Blocked {
		t.Fatalf("restored snapshot = %#v, want blocked usage 100", snapshot)
	}
}

func TestRestoreQuotaWindowRejectsInvalidPersistedState(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		start  time.Time
		limit  int64
		period time.Duration
		used   int64
	}{
		{name: "zero start", limit: 100, period: time.Hour},
		{name: "zero limit", start: start, period: time.Hour},
		{name: "zero period", start: start, limit: 100},
		{name: "negative usage", start: start, limit: 100, period: time.Hour, used: -1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := RestoreQuotaWindow(test.start, test.limit, test.period, test.used); err == nil {
				t.Fatal("RestoreQuotaWindow() error = nil, want error")
			}
		})
	}
}

func TestQuotaWindowRecordsAlreadyAggregatedTraffic(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	window, err := NewQuotaWindow(start, 100, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("NewQuotaWindow() error = %v", err)
	}
	snapshot, err := window.RecordAggregated(start.Add(time.Minute), 40, 60)
	if err != nil {
		t.Fatalf("RecordAggregated() error = %v", err)
	}
	if snapshot.UsedBytes != 100 || !snapshot.Blocked {
		t.Fatalf("RecordAggregated() = %#v, want blocked usage 100", snapshot)
	}
}
