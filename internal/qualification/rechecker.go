package qualification

import (
	"context"
	"errors"
	"fmt"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

type KnownUserProvider interface {
	KnownUsersAfter(ctx context.Context, afterTelegramID int64, limit int) ([]int64, error)
}

type QualificationEvaluator interface {
	Evaluate(ctx context.Context, telegramID int64) (domain.QualificationDecision, error)
}

type DetailedQualificationEvaluator interface {
	EvaluateDetailed(ctx context.Context, telegramID int64) (QualificationEvaluation, error)
}

type RecheckNotifier interface {
	NotifySummary(ctx context.Context, summary RecheckSummary) error
	NotifyFailure(ctx context.Context, err error) error
}

type RecheckSummary struct {
	Checked                   int
	Eligible                  int
	Ineligible                int
	Indeterminate             int
	Revocations               int
	TelegramTemporary         int
	UnknownMembership         int
	UnclassifiedIndeterminate int
}

type Rechecker struct {
	users     KnownUserProvider
	evaluator QualificationEvaluator
	writer    DecisionWriter
	notifier  RecheckNotifier
	batchSize int
}

func NewRechecker(
	users KnownUserProvider,
	evaluator QualificationEvaluator,
	writer DecisionWriter,
	notifier RecheckNotifier,
	batchSize int,
) (*Rechecker, error) {
	if users == nil || evaluator == nil || writer == nil || notifier == nil {
		return nil, errors.New("rechecker dependencies are required")
	}
	if batchSize < 10 || batchSize > 200 {
		return nil, errors.New("recheck batch size must be between 10 and 200")
	}
	return &Rechecker{users: users, evaluator: evaluator, writer: writer, notifier: notifier, batchSize: batchSize}, nil
}

func (rechecker *Rechecker) RunOnce(ctx context.Context) (RecheckSummary, error) {
	if rechecker == nil {
		return RecheckSummary{}, errors.New("rechecker is required")
	}
	var summary RecheckSummary
	var afterTelegramID int64
	for {
		telegramIDs, err := rechecker.users.KnownUsersAfter(ctx, afterTelegramID, rechecker.batchSize)
		if err != nil {
			return summary, rechecker.reportFailure(ctx, fmt.Errorf("list known users: %w", err))
		}
		if err := validateKnownUserPage(telegramIDs, afterTelegramID, rechecker.batchSize); err != nil {
			return summary, rechecker.reportFailure(ctx, err)
		}
		for _, telegramID := range telegramIDs {
			evaluation, err := evaluateForRecheck(ctx, rechecker.evaluator, telegramID)
			if err != nil {
				return summary, rechecker.reportFailure(ctx, fmt.Errorf("evaluate Telegram user %d: %w", telegramID, err))
			}
			summary.Checked++
			switch evaluation.Decision {
			case domain.QualificationEligible:
				summary.Eligible++
			case domain.QualificationIneligible:
				summary.Ineligible++
			case domain.QualificationIndeterminate:
				summary.Indeterminate++
				if evaluation.IndeterminateReason&IndeterminateTelegramTemporary != 0 {
					summary.TelegramTemporary++
				}
				if evaluation.IndeterminateReason&IndeterminateUnknownMembership != 0 {
					summary.UnknownMembership++
				}
				if evaluation.IndeterminateReason == 0 {
					summary.UnclassifiedIndeterminate++
				}
				continue
			default:
				return summary, rechecker.reportFailure(ctx, errors.New("qualification evaluator returned an invalid decision"))
			}
			change, err := rechecker.writer.ApplyQualification(ctx, telegramID, evaluation.Decision)
			if err != nil {
				return summary, rechecker.reportFailure(ctx, fmt.Errorf("persist Telegram user %d qualification: %w", telegramID, err))
			}
			if change.RevokeCredentialsImmediately {
				summary.Revocations++
			}
		}
		if len(telegramIDs) < rechecker.batchSize {
			break
		}
		afterTelegramID = telegramIDs[len(telegramIDs)-1]
	}
	if err := rechecker.notifier.NotifySummary(ctx, summary); err != nil {
		return summary, fmt.Errorf("notify qualification recheck summary: %w", err)
	}
	return summary, nil
}

func evaluateForRecheck(ctx context.Context, evaluator QualificationEvaluator, telegramID int64) (QualificationEvaluation, error) {
	if detailed, ok := evaluator.(DetailedQualificationEvaluator); ok {
		evaluation, err := detailed.EvaluateDetailed(ctx, telegramID)
		if evaluation.IndeterminateReason & ^(IndeterminateTelegramTemporary|IndeterminateUnknownMembership) != 0 {
			return QualificationEvaluation{}, errors.New("qualification evaluator returned an invalid indeterminate reason")
		}
		return evaluation, err
	}
	decision, err := evaluator.Evaluate(ctx, telegramID)
	return QualificationEvaluation{Decision: decision}, err
}

func (rechecker *Rechecker) reportFailure(ctx context.Context, cause error) error {
	notifyErr := rechecker.notifier.NotifyFailure(context.WithoutCancel(ctx), cause)
	if notifyErr != nil {
		return errors.Join(cause, fmt.Errorf("notify qualification recheck failure: %w", notifyErr))
	}
	return cause
}

func validateKnownUserPage(telegramIDs []int64, afterTelegramID int64, limit int) error {
	if len(telegramIDs) > limit {
		return errors.New("known user page exceeds requested limit")
	}
	previous := afterTelegramID
	for _, telegramID := range telegramIDs {
		if telegramID <= previous {
			return errors.New("known user page is not strictly increasing")
		}
		previous = telegramID
	}
	return nil
}
