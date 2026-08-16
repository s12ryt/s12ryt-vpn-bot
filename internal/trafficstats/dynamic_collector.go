package trafficstats

import (
	"context"
	"errors"
)

type StatsAddressProvider interface {
	StatsAddress(context.Context) (string, error)
}

type RPCCloser interface {
	RPC
	Close() error
}

type RPCDialFunc func(string) (RPCCloser, error)

type DynamicCollector struct {
	addresses StatsAddressProvider
	dial      RPCDialFunc
}

func NewDynamicCollector(addresses StatsAddressProvider, dial RPCDialFunc) (*DynamicCollector, error) {
	if addresses == nil || dial == nil {
		return nil, errors.New("dynamic traffic collector dependencies are required")
	}
	return &DynamicCollector{addresses: addresses, dial: dial}, nil
}

func (collector *DynamicCollector) Collect(ctx context.Context) (samples []Sample, err error) {
	if collector == nil || collector.addresses == nil || collector.dial == nil {
		return nil, errors.New("dynamic traffic collector is not configured")
	}
	address, err := collector.addresses.StatsAddress(ctx)
	if err != nil {
		return nil, err
	}
	rpc, err := collector.dial(address)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rpc.Close()) }()
	inner, err := NewCollector(rpc)
	if err != nil {
		return nil, err
	}
	return inner.Collect(ctx)
}

func DialRPC(address string) (RPCCloser, error) {
	return DialGRPC(address)
}
