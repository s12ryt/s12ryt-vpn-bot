package trafficstats

import (
	"context"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	"google.golang.org/grpc"
)

func TestProtoCodecEncodesQueryStatsRequest(t *testing.T) {
	codec := statsProtoCodec{}
	request := &queryStatsRequestWire{
		Reset:    true,
		Patterns: []string{"^user>>>", "downlink$"},
		Regexp:   true,
	}
	encoded, err := codec.Marshal(request)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	const wantHex = "10011a085e757365723e3e3e1a09646f776e6c696e6b242001"
	if got := hex.EncodeToString(encoded); got != wantHex {
		t.Fatalf("Marshal() = %s, want %s", got, wantHex)
	}
}

func TestProtoCodecDecodesQueryStatsResponse(t *testing.T) {
	codec := statsProtoCodec{}
	// Two Stat messages plus one unknown top-level varint field.
	wire, err := hex.DecodeString("0a220a1e757365723e3e3e313030313e3e3e747261666669633e3e3e75706c696e6b100b0a250a20757365723e3e3e323030323e3e3e747261666669633e3e3e646f776e6c696e6b108f4e2801")
	if err != nil {
		t.Fatal(err)
	}
	var response queryStatsResponseWire
	if err := codec.Unmarshal(wire, &response); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	want := []Stat{
		{Name: "user>>>1001>>>traffic>>>uplink", Value: 11},
		{Name: "user>>>2002>>>traffic>>>downlink", Value: 9999},
	}
	if !reflect.DeepEqual(response.Stats, want) {
		t.Fatalf("Unmarshal() stats = %#v, want %#v", response.Stats, want)
	}
}

func TestProtoCodecRejectsMalformedResponseWithoutPartialStats(t *testing.T) {
	codec := statsProtoCodec{}
	response := queryStatsResponseWire{Stats: []Stat{{Name: "existing", Value: 1}}}
	if err := codec.Unmarshal([]byte{0x0a, 0x05, 0x0a}, &response); err == nil {
		t.Fatal("Unmarshal() error = nil, want error")
	}
	if response.Stats != nil {
		t.Fatalf("Unmarshal() stats = %#v, want nil", response.Stats)
	}
}

func TestGRPCRPCInvokesCompatibleStatsService(t *testing.T) {
	invoker := &fakeUnaryInvoker{response: []Stat{{Name: "user>>>1001>>>traffic>>>uplink", Value: 42}}}
	rpc, err := newGRPCRPC(invoker)
	if err != nil {
		t.Fatalf("newGRPCRPC() error = %v", err)
	}
	request := QueryRequest{Patterns: []string{"user>>>"}, Reset: true, Regexp: false}
	stats, err := rpc.QueryStats(context.Background(), request)
	if err != nil {
		t.Fatalf("QueryStats() error = %v", err)
	}
	if invoker.method != "/v2ray.core.app.stats.command.StatsService/QueryStats" {
		t.Fatalf("Invoke() method = %q", invoker.method)
	}
	if !reflect.DeepEqual(invoker.request, queryStatsRequestWire{Patterns: []string{"user>>>"}, Reset: true}) {
		t.Fatalf("Invoke() request = %#v", invoker.request)
	}
	if !reflect.DeepEqual(stats, invoker.response) {
		t.Fatalf("QueryStats() = %#v, want %#v", stats, invoker.response)
	}
}

func TestDialGRPCRejectsNonLoopbackAddress(t *testing.T) {
	for _, address := range []string{"", "127.0.0.1", "0.0.0.0:10085", "203.0.113.10:10085", "localhost:10085", "127.0.0.1:0", "127.0.0.1:nope", "127.0.0.1:65536"} {
		t.Run(address, func(t *testing.T) {
			if _, err := DialGRPC(address); err == nil {
				t.Fatalf("DialGRPC(%q) error = nil, want error", address)
			}
		})
	}
}

func TestGRPCRPCPreservesInvokeFailure(t *testing.T) {
	wantErr := errors.New("unavailable")
	rpc, err := newGRPCRPC(&fakeUnaryInvoker{err: wantErr})
	if err != nil {
		t.Fatalf("newGRPCRPC() error = %v", err)
	}
	if _, err := rpc.QueryStats(context.Background(), QueryRequest{}); !errors.Is(err, wantErr) {
		t.Fatalf("QueryStats() error = %v, want wrapping %v", err, wantErr)
	}
}

type fakeUnaryInvoker struct {
	method   string
	request  queryStatsRequestWire
	response []Stat
	err      error
}

func (invoker *fakeUnaryInvoker) Invoke(_ context.Context, method string, args, reply any, _ ...grpc.CallOption) error {
	invoker.method = method
	invoker.request = *args.(*queryStatsRequestWire)
	if invoker.err != nil {
		return invoker.err
	}
	reply.(*queryStatsResponseWire).Stats = append([]Stat(nil), invoker.response...)
	return nil
}
