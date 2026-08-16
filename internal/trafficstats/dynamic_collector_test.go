package trafficstats

import (
	"context"
	"errors"
	"testing"
)

func TestDynamicCollectorReloadsAddressAndClosesEveryConnection(t *testing.T) {
	provider := &addressProviderStub{addresses: []string{"127.0.0.1:8080", "[::1]:8081"}}
	dialer := &rpcDialerStub{}
	collector, err := NewDynamicCollector(provider, dialer.Dial)
	if err != nil {
		t.Fatalf("NewDynamicCollector() error = %v", err)
	}
	for range 2 {
		if _, err := collector.Collect(context.Background()); err != nil {
			t.Fatalf("Collect() error = %v", err)
		}
	}
	if len(dialer.addresses) != 2 || dialer.addresses[0] == dialer.addresses[1] || dialer.closed != 2 {
		t.Fatalf("addresses=%v closed=%d", dialer.addresses, dialer.closed)
	}
}

func TestDynamicCollectorDoesNotDialWhenAddressLoadFails(t *testing.T) {
	want := errors.New("not configured")
	dialer := &rpcDialerStub{}
	collector, _ := NewDynamicCollector(&addressProviderStub{err: want}, dialer.Dial)
	if _, err := collector.Collect(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(dialer.addresses) != 0 {
		t.Fatalf("dialed = %v", dialer.addresses)
	}
}

type addressProviderStub struct {
	addresses []string
	err       error
	index     int
}

func (stub *addressProviderStub) StatsAddress(context.Context) (string, error) {
	if stub.err != nil {
		return "", stub.err
	}
	address := stub.addresses[stub.index]
	stub.index++
	return address, nil
}

type rpcDialerStub struct {
	addresses []string
	closed    int
}

func (stub *rpcDialerStub) Dial(address string) (RPCCloser, error) {
	stub.addresses = append(stub.addresses, address)
	return &rpcCloserStub{owner: stub}, nil
}

type rpcCloserStub struct{ owner *rpcDialerStub }

func (stub *rpcCloserStub) QueryStats(context.Context, QueryRequest) ([]Stat, error) { return nil, nil }
func (stub *rpcCloserStub) Close() error                                             { stub.owner.closed++; return nil }
