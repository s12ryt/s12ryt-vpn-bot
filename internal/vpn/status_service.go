package vpn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

type StatusUserReader interface {
	FindUser(ctx context.Context, telegramID int64) (domain.UserOverview, error)
}

type Status struct {
	Overview        domain.UserOverview
	SubscriptionURL string
	ResetsAt        time.Time
}

type StatusService struct {
	users       StatusUserReader
	credentials ActiveCredentialReader
	links       SubscriptionLinkBuilder
	period      time.Duration
}

func NewStatusService(users StatusUserReader, credentials ActiveCredentialReader, links SubscriptionLinkBuilder, period time.Duration) *StatusService {
	return &StatusService{users: users, credentials: credentials, links: links, period: period}
}

func (service *StatusService) GetStatus(ctx context.Context, telegramID int64) (Status, error) {
	if service == nil || service.users == nil || service.credentials == nil || service.links == nil || service.period <= 0 {
		return Status{}, errors.New("VPN status service dependencies are required")
	}
	if telegramID <= 0 {
		return Status{}, errors.New("Telegram ID must be positive")
	}
	overview, err := service.users.FindUser(ctx, telegramID)
	if err != nil {
		return Status{}, fmt.Errorf("load VPN status: %w", err)
	}
	if overview.TelegramID != telegramID {
		return Status{}, errors.New("VPN status owner is invalid")
	}
	status := Status{Overview: overview}
	if !overview.PeriodStartedAt.IsZero() {
		status.ResetsAt = overview.PeriodStartedAt.Add(service.period)
	}
	if overview.Status != domain.AccessStatusActive || !overview.Eligible {
		return status, nil
	}
	bundle, err := service.credentials.FindActiveByTelegramID(ctx, telegramID)
	if err != nil {
		return Status{}, fmt.Errorf("load VPN status credentials: %w", err)
	}
	status.SubscriptionURL, err = service.links.SubscriptionURL(bundle.SubscriptionToken)
	if err != nil {
		return Status{}, fmt.Errorf("build VPN status subscription URL: %w", err)
	}
	return status, nil
}
