package vpn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

var ErrQualificationUnavailable = errors.New("qualification is temporarily unavailable")

type QualificationEvaluator interface {
	Evaluate(ctx context.Context, telegramID int64) (domain.QualificationDecision, error)
}

type QualificationWriter interface {
	ApplyQualification(ctx context.Context, telegramID int64, decision domain.QualificationDecision) (domain.AccessChange, error)
}

type AccessProvisioner interface {
	Claim(ctx context.Context, telegramID int64, now time.Time) (domain.ProvisionedAccess, error)
}

type ActiveCredentialReader interface {
	FindActiveByTelegramID(ctx context.Context, telegramID int64) (domain.CredentialBundle, error)
}

type SubscriptionLinkBuilder interface {
	SubscriptionURL(token string) (string, error)
}

type Access struct {
	SubscriptionURL string
	NewlyIssued     bool
}

type AccessService struct {
	evaluator   QualificationEvaluator
	writer      QualificationWriter
	provisioner AccessProvisioner
	credentials ActiveCredentialReader
	links       SubscriptionLinkBuilder
	now         func() time.Time
}

func NewAccessService(
	evaluator QualificationEvaluator,
	writer QualificationWriter,
	provisioner AccessProvisioner,
	credentials ActiveCredentialReader,
	links SubscriptionLinkBuilder,
	now func() time.Time,
) *AccessService {
	return &AccessService{
		evaluator: evaluator, writer: writer, provisioner: provisioner,
		credentials: credentials, links: links, now: now,
	}
}

func (service *AccessService) GetOrClaim(ctx context.Context, telegramID int64) (Access, error) {
	if service == nil || service.evaluator == nil || service.writer == nil || service.provisioner == nil ||
		service.credentials == nil || service.links == nil || service.now == nil {
		return Access{}, errors.New("VPN access service dependencies are required")
	}
	if telegramID <= 0 {
		return Access{}, errors.New("Telegram ID must be positive")
	}
	decision, err := service.evaluator.Evaluate(ctx, telegramID)
	if err != nil {
		return Access{}, fmt.Errorf("evaluate VPN qualification: %w", err)
	}
	switch decision {
	case domain.QualificationIndeterminate:
		return Access{}, ErrQualificationUnavailable
	case domain.QualificationIneligible:
		if _, err := service.writer.ApplyQualification(ctx, telegramID, decision); err != nil {
			return Access{}, fmt.Errorf("persist VPN qualification: %w", err)
		}
		return Access{}, domain.ErrNotEligible
	case domain.QualificationEligible:
		if _, err := service.writer.ApplyQualification(ctx, telegramID, decision); err != nil {
			return Access{}, fmt.Errorf("persist VPN qualification: %w", err)
		}
	default:
		return Access{}, errors.New("qualification decision is invalid")
	}

	newlyIssued := true
	provisioned, err := service.provisioner.Claim(ctx, telegramID, service.now())
	var bundle domain.CredentialBundle
	if errors.Is(err, domain.ErrAlreadyActive) {
		newlyIssued = false
		bundle, err = service.credentials.FindActiveByTelegramID(ctx, telegramID)
	} else if err == nil {
		bundle = provisioned.Credentials
	}
	if err != nil {
		return Access{}, fmt.Errorf("obtain VPN credentials: %w", err)
	}
	link, err := service.links.SubscriptionURL(bundle.SubscriptionToken)
	if err != nil {
		return Access{}, fmt.Errorf("build subscription URL: %w", err)
	}
	return Access{SubscriptionURL: link, NewlyIssued: newlyIssued}, nil
}
