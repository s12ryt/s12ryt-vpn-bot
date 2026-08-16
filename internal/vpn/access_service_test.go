package vpn

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

func TestAccessServiceRevalidatesAndClaimsBeforeReturningSubscription(t *testing.T) {
	bundle := serviceCredentialBundle("new-token")
	evaluator := &qualificationEvaluatorStub{decision: domain.QualificationEligible}
	writer := &qualificationWriterStub{}
	provisioner := &accessProvisionerStub{provisioned: domain.ProvisionedAccess{Credentials: bundle}}
	links := &subscriptionLinkBuilderStub{url: "https://vpn.example.com/sub/new-token"}
	service := NewAccessService(evaluator, writer, provisioner, &activeCredentialReaderStub{}, links, func() time.Time {
		return time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	})

	access, err := service.GetOrClaim(context.Background(), 12345)
	if err != nil {
		t.Fatalf("GetOrClaim() error = %v", err)
	}
	if access.SubscriptionURL != links.url || !access.NewlyIssued {
		t.Fatalf("GetOrClaim() = %#v", access)
	}
	if evaluator.telegramID != 12345 || writer.decision != domain.QualificationEligible || provisioner.calls != 1 {
		t.Fatalf("flow evaluator=%d writer=%q provisioner calls=%d", evaluator.telegramID, writer.decision, provisioner.calls)
	}
	if links.token != bundle.SubscriptionToken {
		t.Fatalf("link token = %q, want issued token", links.token)
	}
}

func TestAccessServiceReturnsExistingSubscriptionForActiveUser(t *testing.T) {
	bundle := serviceCredentialBundle("existing-token")
	reader := &activeCredentialReaderStub{bundle: bundle}
	service := NewAccessService(
		&qualificationEvaluatorStub{decision: domain.QualificationEligible},
		&qualificationWriterStub{},
		&accessProvisionerStub{err: domain.ErrAlreadyActive},
		reader,
		&subscriptionLinkBuilderStub{url: "https://vpn.example.com/sub/existing-token"},
		time.Now,
	)

	access, err := service.GetOrClaim(context.Background(), 12345)
	if err != nil {
		t.Fatalf("GetOrClaim() error = %v", err)
	}
	if access.NewlyIssued || reader.telegramID != 12345 {
		t.Fatalf("GetOrClaim() = %#v reader ID=%d", access, reader.telegramID)
	}
}

func TestAccessServiceDoesNotIssueWithoutExplicitEligibility(t *testing.T) {
	for _, decision := range []domain.QualificationDecision{domain.QualificationIndeterminate, domain.QualificationIneligible} {
		writer := &qualificationWriterStub{}
		provisioner := &accessProvisionerStub{}
		service := NewAccessService(
			&qualificationEvaluatorStub{decision: decision}, writer, provisioner,
			&activeCredentialReaderStub{}, &subscriptionLinkBuilderStub{}, time.Now,
		)

		_, err := service.GetOrClaim(context.Background(), 12345)
		if decision == domain.QualificationIndeterminate && !errors.Is(err, ErrQualificationUnavailable) {
			t.Fatalf("indeterminate GetOrClaim() error = %v", err)
		}
		if decision == domain.QualificationIneligible && !errors.Is(err, domain.ErrNotEligible) {
			t.Fatalf("ineligible GetOrClaim() error = %v", err)
		}
		if provisioner.calls != 0 {
			t.Fatalf("decision %q called provisioner", decision)
		}
		if decision == domain.QualificationIndeterminate && writer.calls != 0 {
			t.Fatal("indeterminate decision was persisted")
		}
		if decision == domain.QualificationIneligible && (writer.calls != 1 || writer.decision != decision) {
			t.Fatalf("ineligible writer calls=%d decision=%q", writer.calls, writer.decision)
		}
	}
}

type qualificationEvaluatorStub struct {
	decision   domain.QualificationDecision
	err        error
	telegramID int64
}

func (stub *qualificationEvaluatorStub) Evaluate(_ context.Context, telegramID int64) (domain.QualificationDecision, error) {
	stub.telegramID = telegramID
	return stub.decision, stub.err
}

type qualificationWriterStub struct {
	decision domain.QualificationDecision
	err      error
	calls    int
}

func (stub *qualificationWriterStub) ApplyQualification(_ context.Context, _ int64, decision domain.QualificationDecision) (domain.AccessChange, error) {
	stub.calls++
	stub.decision = decision
	return domain.AccessChange{}, stub.err
}

type accessProvisionerStub struct {
	provisioned domain.ProvisionedAccess
	err         error
	calls       int
}

func (stub *accessProvisionerStub) Claim(context.Context, int64, time.Time) (domain.ProvisionedAccess, error) {
	stub.calls++
	return stub.provisioned, stub.err
}

type activeCredentialReaderStub struct {
	bundle     domain.CredentialBundle
	err        error
	telegramID int64
}

func (stub *activeCredentialReaderStub) FindActiveByTelegramID(_ context.Context, telegramID int64) (domain.CredentialBundle, error) {
	stub.telegramID = telegramID
	return stub.bundle, stub.err
}

type subscriptionLinkBuilderStub struct {
	url   string
	err   error
	token string
}

func (stub *subscriptionLinkBuilderStub) SubscriptionURL(token string) (string, error) {
	stub.token = token
	return stub.url, stub.err
}

func serviceCredentialBundle(token string) domain.CredentialBundle {
	return domain.CredentialBundle{SubscriptionToken: token}
}
