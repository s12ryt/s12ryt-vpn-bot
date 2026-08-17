package main

import (
	"context"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

// dashboardAdapter composes the existing stores into the operational
// overview without duplicating any query logic.
type dashboardAdapter struct {
	users interface {
		Statistics(context.Context) (domain.UserStatistics, error)
	}
	tls interface {
		Issued(context.Context) (bool, error)
	}
	core interface {
		GetCore(context.Context) (domain.CoreSettingsOverview, error)
	}
}

func (adapter *dashboardAdapter) Statistics(ctx context.Context) (domain.UserStatistics, error) {
	return adapter.users.Statistics(ctx)
}

func (adapter *dashboardAdapter) TLSIssued(ctx context.Context) (bool, error) {
	return adapter.tls.Issued(ctx)
}

func (adapter *dashboardAdapter) CoreConfigured(ctx context.Context) (bool, error) {
	overview, err := adapter.core.GetCore(ctx)
	if err != nil {
		return false, err
	}
	return overview.Configured, nil
}
