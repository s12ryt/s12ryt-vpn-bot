package domain

import (
	"errors"
	"time"
)

type TrafficAccountingChange struct {
	Quota                         QuotaSnapshot
	RevokeCredentialsImmediately  bool
	RestoreCredentialsImmediately bool
}

func RecordAccountTraffic(account *AccessAccount, quota *QuotaWindow, sample TrafficSample) (TrafficAccountingChange, error) {
	return recordAccountTraffic(account, quota, sample.ObservedAt, sample.Uplink, sample.Downlink, func(window *QuotaWindow) (QuotaSnapshot, error) {
		return window.Record(sample)
	})
}

func RecordAggregatedAccountTraffic(
	account *AccessAccount,
	quota *QuotaWindow,
	observedAt time.Time,
	uplink int64,
	downlink int64,
) (TrafficAccountingChange, error) {
	return recordAccountTraffic(account, quota, observedAt, uplink, downlink, func(window *QuotaWindow) (QuotaSnapshot, error) {
		return window.RecordAggregated(observedAt, uplink, downlink)
	})
}

func recordAccountTraffic(
	account *AccessAccount,
	quota *QuotaWindow,
	observedAt time.Time,
	uplink int64,
	downlink int64,
	record func(*QuotaWindow) (QuotaSnapshot, error),
) (TrafficAccountingChange, error) {
	if account == nil {
		return TrafficAccountingChange{}, errors.New("access account is required")
	}
	if quota == nil {
		return TrafficAccountingChange{}, errors.New("quota window is required")
	}
	if record == nil {
		return TrafficAccountingChange{}, errors.New("traffic recorder is required")
	}

	accountCopy := *account
	quotaCopy := *quota
	wasBlocked := quotaCopy.snapshot().Blocked
	snapshot, err := record(&quotaCopy)
	if err != nil {
		return TrafficAccountingChange{}, err
	}
	sampleBytes, ok := addNonNegative(uplink, downlink)
	if !ok {
		return TrafficAccountingChange{}, errors.New("traffic counters overflow")
	}
	if sampleBytes > 0 {
		if err := accountCopy.RecordVPNActivity(observedAt, sampleBytes); err != nil {
			return TrafficAccountingChange{}, err
		}
	}

	*account = accountCopy
	*quota = quotaCopy
	return TrafficAccountingChange{
		Quota:                         snapshot,
		RevokeCredentialsImmediately:  !wasBlocked && snapshot.Blocked,
		RestoreCredentialsImmediately: wasBlocked && !snapshot.Blocked,
	}, nil
}
