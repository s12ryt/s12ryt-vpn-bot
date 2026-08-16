package trafficstats

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protowire"
)

const queryStatsMethod = "/v2ray.core.app.stats.command.StatsService/QueryStats"

type queryStatsRequestWire struct {
	Patterns []string
	Reset    bool
	Regexp   bool
}

type queryStatsResponseWire struct {
	Stats []Stat
}

type statsProtoCodec struct{}

func (statsProtoCodec) Name() string { return "proto" }

func (statsProtoCodec) Marshal(value any) ([]byte, error) {
	request, ok := value.(*queryStatsRequestWire)
	if !ok || request == nil {
		return nil, errors.New("unsupported stats protobuf message")
	}
	var encoded []byte
	if request.Reset {
		encoded = protowire.AppendTag(encoded, 2, protowire.VarintType)
		encoded = protowire.AppendVarint(encoded, 1)
	}
	for _, pattern := range request.Patterns {
		encoded = protowire.AppendTag(encoded, 3, protowire.BytesType)
		encoded = protowire.AppendString(encoded, pattern)
	}
	if request.Regexp {
		encoded = protowire.AppendTag(encoded, 4, protowire.VarintType)
		encoded = protowire.AppendVarint(encoded, 1)
	}
	return encoded, nil
}

func (statsProtoCodec) Unmarshal(encoded []byte, value any) error {
	response, ok := value.(*queryStatsResponseWire)
	if !ok || response == nil {
		return errors.New("unsupported stats protobuf message")
	}
	response.Stats = nil
	var stats []Stat
	for len(encoded) > 0 {
		number, wireType, tagLength := protowire.ConsumeTag(encoded)
		if tagLength < 0 {
			return errors.New("invalid stats protobuf tag")
		}
		encoded = encoded[tagLength:]
		if number == 1 && wireType == protowire.BytesType {
			message, length := protowire.ConsumeBytes(encoded)
			if length < 0 {
				return errors.New("invalid stats protobuf message")
			}
			stat, err := decodeStat(message)
			if err != nil {
				return err
			}
			stats = append(stats, stat)
			encoded = encoded[length:]
			continue
		}
		length := protowire.ConsumeFieldValue(number, wireType, encoded)
		if length < 0 {
			return errors.New("invalid stats protobuf field")
		}
		encoded = encoded[length:]
	}
	response.Stats = stats
	return nil
}

func decodeStat(encoded []byte) (Stat, error) {
	var stat Stat
	var nameSet, valueSet bool
	for len(encoded) > 0 {
		number, wireType, tagLength := protowire.ConsumeTag(encoded)
		if tagLength < 0 {
			return Stat{}, errors.New("invalid stat protobuf tag")
		}
		encoded = encoded[tagLength:]
		switch {
		case number == 1 && wireType == protowire.BytesType:
			if nameSet {
				return Stat{}, errors.New("duplicate stat name")
			}
			name, length := protowire.ConsumeString(encoded)
			if length < 0 {
				return Stat{}, errors.New("invalid stat name")
			}
			stat.Name = name
			nameSet = true
			encoded = encoded[length:]
		case number == 2 && wireType == protowire.VarintType:
			if valueSet {
				return Stat{}, errors.New("duplicate stat value")
			}
			value, length := protowire.ConsumeVarint(encoded)
			if length < 0 {
				return Stat{}, errors.New("invalid stat value")
			}
			stat.Value = int64(value)
			valueSet = true
			encoded = encoded[length:]
		default:
			length := protowire.ConsumeFieldValue(number, wireType, encoded)
			if length < 0 {
				return Stat{}, errors.New("invalid stat protobuf field")
			}
			encoded = encoded[length:]
		}
	}
	if !nameSet || !valueSet {
		return Stat{}, errors.New("incomplete stat protobuf message")
	}
	return stat, nil
}

type unaryInvoker interface {
	Invoke(context.Context, string, any, any, ...grpc.CallOption) error
}

type GRPCRPC struct {
	invoker unaryInvoker
	closer  interface{ Close() error }
}

func newGRPCRPC(invoker unaryInvoker) (*GRPCRPC, error) {
	if invoker == nil {
		return nil, errors.New("gRPC invoker is required")
	}
	return &GRPCRPC{invoker: invoker}, nil
}

func DialGRPC(address string) (*GRPCRPC, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return nil, errors.New("stats address must be a loopback host:port")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() {
		return nil, errors.New("stats address must use a loopback IP")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, errors.New("stats address has an invalid port")
	}
	connection, err := grpc.NewClient(
		"passthrough:///"+net.JoinHostPort(ip.String(), port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("create stats gRPC client: %w", err)
	}
	return &GRPCRPC{invoker: connection, closer: connection}, nil
}

func (rpc *GRPCRPC) QueryStats(ctx context.Context, request QueryRequest) ([]Stat, error) {
	if rpc == nil || rpc.invoker == nil {
		return nil, errors.New("stats gRPC client is not initialized")
	}
	wireRequest := &queryStatsRequestWire{
		Patterns: append([]string(nil), request.Patterns...),
		Reset:    request.Reset,
		Regexp:   request.Regexp,
	}
	var response queryStatsResponseWire
	if err := rpc.invoker.Invoke(ctx, queryStatsMethod, wireRequest, &response, grpc.ForceCodec(statsProtoCodec{})); err != nil {
		return nil, fmt.Errorf("query stats RPC: %w", err)
	}
	return append([]Stat(nil), response.Stats...), nil
}

func (rpc *GRPCRPC) Close() error {
	if rpc == nil || rpc.closer == nil {
		return nil
	}
	return rpc.closer.Close()
}
