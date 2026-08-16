package trafficstats

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
)

const userTrafficPattern = `^user>>>[1-9][0-9]*>>>traffic>>>(uplink|downlink)$`

var userTrafficName = regexp.MustCompile(`^user>>>([1-9][0-9]*)>>>traffic>>>(uplink|downlink)$`)

type QueryRequest struct {
	Patterns []string
	Reset    bool
	Regexp   bool
}

type Stat struct {
	Name  string
	Value int64
}

type Sample struct {
	TelegramID int64 `json:"telegram_id"`
	Uplink     int64 `json:"uplink"`
	Downlink   int64 `json:"downlink"`
}

type RPC interface {
	QueryStats(context.Context, QueryRequest) ([]Stat, error)
}

type Collector struct {
	rpc RPC
}

func NewCollector(rpc RPC) (*Collector, error) {
	if rpc == nil {
		return nil, errors.New("stats RPC is required")
	}
	return &Collector{rpc: rpc}, nil
}

func (collector *Collector) Collect(ctx context.Context) ([]Sample, error) {
	if collector == nil || collector.rpc == nil {
		return nil, errors.New("collector is not initialized")
	}
	stats, err := collector.rpc.QueryStats(ctx, QueryRequest{
		Patterns: []string{userTrafficPattern},
		Reset:    true,
		Regexp:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("query user traffic: %w", err)
	}

	type counters struct {
		uplinkSet   bool
		downlinkSet bool
		uplink      int64
		downlink    int64
	}
	byUser := make(map[int64]counters)
	for _, stat := range stats {
		matches := userTrafficName.FindStringSubmatch(stat.Name)
		if len(matches) != 3 || stat.Value < 0 {
			return nil, errors.New("stats response contains an invalid counter")
		}
		telegramID, err := strconv.ParseInt(matches[1], 10, 64)
		if err != nil || telegramID <= 0 {
			return nil, errors.New("stats response contains an invalid user")
		}
		current := byUser[telegramID]
		switch matches[2] {
		case "uplink":
			if current.uplinkSet {
				return nil, errors.New("stats response contains a duplicate counter")
			}
			current.uplinkSet = true
			current.uplink = stat.Value
		case "downlink":
			if current.downlinkSet {
				return nil, errors.New("stats response contains a duplicate counter")
			}
			current.downlinkSet = true
			current.downlink = stat.Value
		default:
			return nil, errors.New("stats response contains an invalid direction")
		}
		if current.uplink > math.MaxInt64-current.downlink {
			return nil, errors.New("stats response counters overflow")
		}
		byUser[telegramID] = current
	}

	ids := make([]int64, 0, len(byUser))
	for telegramID := range byUser {
		ids = append(ids, telegramID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	samples := make([]Sample, 0, len(ids))
	for _, telegramID := range ids {
		current := byUser[telegramID]
		samples = append(samples, Sample{
			TelegramID: telegramID,
			Uplink:     current.uplink,
			Downlink:   current.downlink,
		})
	}
	return samples, nil
}
