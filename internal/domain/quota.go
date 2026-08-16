package domain

import (
	"errors"
	"math"
	"time"
)

type Protocol string

const (
	ProtocolVLESS     Protocol = "vless"
	ProtocolHysteria2 Protocol = "hysteria2"
	ProtocolTUIC      Protocol = "tuic"
	ProtocolAnyTLS    Protocol = "anytls"
)

type AddressFamily string

const (
	AddressFamilyIPv4 AddressFamily = "ipv4"
	AddressFamilyIPv6 AddressFamily = "ipv6"
)

type TrafficSample struct {
	ObservedAt time.Time
	Protocol   Protocol
	Family     AddressFamily
	Uplink     int64
	Downlink   int64
}

type QuotaSnapshot struct {
	PeriodStartedAt time.Time
	UsedBytes       int64
	Blocked         bool
}

type QuotaWindow struct {
	periodStartedAt time.Time
	limitBytes      int64
	period          time.Duration
	usedBytes       int64
}

func NewQuotaWindow(start time.Time, limitBytes int64, period time.Duration) (*QuotaWindow, error) {
	if start.IsZero() {
		return nil, errors.New("quota period start is required")
	}
	if limitBytes <= 0 {
		return nil, errors.New("quota limit must be positive")
	}
	if period <= 0 {
		return nil, errors.New("quota period must be positive")
	}

	return &QuotaWindow{
		periodStartedAt: start,
		limitBytes:      limitBytes,
		period:          period,
	}, nil
}

func RestoreQuotaWindow(start time.Time, limitBytes int64, period time.Duration, usedBytes int64) (*QuotaWindow, error) {
	window, err := NewQuotaWindow(start, limitBytes, period)
	if err != nil {
		return nil, err
	}
	if usedBytes < 0 {
		return nil, errors.New("quota usage cannot be negative")
	}
	window.usedBytes = usedBytes
	return window, nil
}

func (window *QuotaWindow) Record(sample TrafficSample) (QuotaSnapshot, error) {
	if !sample.Protocol.valid() {
		return QuotaSnapshot{}, errors.New("traffic protocol is invalid")
	}
	if !sample.Family.valid() {
		return QuotaSnapshot{}, errors.New("traffic address family is invalid")
	}
	return window.recordCounters(sample.ObservedAt, sample.Uplink, sample.Downlink)
}

func (window *QuotaWindow) RecordAggregated(observedAt time.Time, uplink, downlink int64) (QuotaSnapshot, error) {
	return window.recordCounters(observedAt, uplink, downlink)
}

func (window *QuotaWindow) recordCounters(observedAt time.Time, uplink, downlink int64) (QuotaSnapshot, error) {
	if observedAt.IsZero() || observedAt.Before(window.periodStartedAt) {
		return QuotaSnapshot{}, errors.New("traffic sample predates the quota period")
	}
	if uplink < 0 || downlink < 0 {
		return QuotaSnapshot{}, errors.New("traffic counters cannot be negative")
	}
	sampleBytes, ok := addNonNegative(uplink, downlink)
	if !ok {
		return QuotaSnapshot{}, errors.New("traffic counters overflow")
	}

	window.advanceTo(observedAt)

	usedBytes, ok := addNonNegative(window.usedBytes, sampleBytes)
	if !ok {
		return QuotaSnapshot{}, errors.New("quota usage overflow")
	}
	window.usedBytes = usedBytes

	return window.snapshot(), nil
}

func (window *QuotaWindow) Advance(now time.Time) (QuotaSnapshot, error) {
	if now.IsZero() || now.Before(window.periodStartedAt) {
		return QuotaSnapshot{}, errors.New("quota advancement timestamp is invalid")
	}
	window.advanceTo(now)
	return window.snapshot(), nil
}

func (window *QuotaWindow) AdjustLimit(limitBytes int64) (QuotaSnapshot, error) {
	if limitBytes <= 0 {
		return QuotaSnapshot{}, errors.New("quota limit must be positive")
	}
	window.limitBytes = limitBytes
	return window.snapshot(), nil
}

func (window *QuotaWindow) snapshot() QuotaSnapshot {
	return QuotaSnapshot{
		PeriodStartedAt: window.periodStartedAt,
		UsedBytes:       window.usedBytes,
		Blocked:         window.usedBytes >= window.limitBytes,
	}
}

func (window *QuotaWindow) advanceTo(now time.Time) {
	if elapsed := now.Sub(window.periodStartedAt); elapsed >= window.period {
		elapsedPeriods := elapsed / window.period
		window.periodStartedAt = window.periodStartedAt.Add(elapsedPeriods * window.period)
		window.usedBytes = 0
	}
}

func (protocol Protocol) valid() bool {
	switch protocol {
	case ProtocolVLESS, ProtocolHysteria2, ProtocolTUIC, ProtocolAnyTLS:
		return true
	default:
		return false
	}
}

func (family AddressFamily) valid() bool {
	return family == AddressFamilyIPv4 || family == AddressFamilyIPv6
}

func addNonNegative(left, right int64) (int64, bool) {
	if right > math.MaxInt64-left {
		return 0, false
	}
	return left + right, true
}
