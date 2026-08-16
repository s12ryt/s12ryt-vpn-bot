package domain

import (
	"testing"
	"time"
)

func TestRecordAccountTrafficUpdatesActivityAndRevokesAtSharedLimit(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	account := activeAccountForTest(t, 12345, start)
	quota, err := NewQuotaWindow(start, 100, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("NewQuotaWindow() error = %v", err)
	}
	observedAt := start.Add(time.Hour)

	change, err := RecordAccountTraffic(account, quota, TrafficSample{
		ObservedAt: observedAt,
		Protocol:   ProtocolAnyTLS,
		Family:     AddressFamilyIPv6,
		Uplink:     40,
		Downlink:   60,
	})
	if err != nil {
		t.Fatalf("RecordAccountTraffic() error = %v", err)
	}
	if !change.RevokeCredentialsImmediately || change.RestoreCredentialsImmediately {
		t.Fatalf("RecordAccountTraffic() change = %#v, want immediate revoke only", change)
	}
	if change.Quota.UsedBytes != 100 || !change.Quota.Blocked {
		t.Fatalf("RecordAccountTraffic() quota = %#v, want blocked at 100", change.Quota)
	}
	if got := account.Snapshot().LastVPNActivityAt; !got.Equal(observedAt) {
		t.Fatalf("LastVPNActivityAt = %v, want %v", got, observedAt)
	}
}

func TestRecordAccountTrafficFailureMutatesNeitherAccountNorQuota(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	account := activeAccountForTest(t, 12345, start)
	quota, err := NewQuotaWindow(start, 100, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("NewQuotaWindow() error = %v", err)
	}
	accountBefore := account.Snapshot()
	quotaBefore, err := quota.Advance(start)
	if err != nil {
		t.Fatalf("Advance() setup error = %v", err)
	}

	if _, err := RecordAccountTraffic(account, quota, TrafficSample{
		ObservedAt: start.Add(-time.Second),
		Protocol:   ProtocolVLESS,
		Family:     AddressFamilyIPv4,
		Uplink:     1,
	}); err == nil {
		t.Fatal("RecordAccountTraffic() error = nil, want stale sample error")
	}
	if accountAfter := account.Snapshot(); accountAfter != accountBefore {
		t.Fatalf("account mutated from %#v to %#v", accountBefore, accountAfter)
	}
	quotaAfter, err := quota.Advance(start)
	if err != nil {
		t.Fatalf("Advance() inspection error = %v", err)
	}
	if quotaAfter != quotaBefore {
		t.Fatalf("quota mutated from %#v to %#v", quotaBefore, quotaAfter)
	}
}

func TestRecordAccountTrafficZeroByteSampleDoesNotCountAsActivity(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	account := activeAccountForTest(t, 12345, start)
	quota, err := NewQuotaWindow(start, 100, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("NewQuotaWindow() error = %v", err)
	}

	if _, err := RecordAccountTraffic(account, quota, TrafficSample{
		ObservedAt: start.Add(time.Hour),
		Protocol:   ProtocolTUIC,
		Family:     AddressFamilyIPv6,
	}); err != nil {
		t.Fatalf("RecordAccountTraffic() error = %v", err)
	}
	if got := account.Snapshot().LastVPNActivityAt; !got.Equal(start) {
		t.Fatalf("LastVPNActivityAt = %v, want unchanged %v", got, start)
	}
}

func TestRecordAggregatedAccountTrafficUpdatesSharedQuotaWithoutInventedDimension(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	account := activeAccountForTest(t, 456, start)
	quota, err := NewQuotaWindow(start, 100, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("NewQuotaWindow() error = %v", err)
	}
	observedAt := start.Add(time.Minute)
	change, err := RecordAggregatedAccountTraffic(account, quota, observedAt, 45, 55)
	if err != nil {
		t.Fatalf("RecordAggregatedAccountTraffic() error = %v", err)
	}
	if change.Quota.UsedBytes != 100 || !change.RevokeCredentialsImmediately {
		t.Fatalf("RecordAggregatedAccountTraffic() = %#v, want immediate block", change)
	}
	if got := account.Snapshot().LastVPNActivityAt; !got.Equal(observedAt) {
		t.Fatalf("LastVPNActivityAt = %v, want %v", got, observedAt)
	}
}
