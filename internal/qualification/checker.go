package qualification

import (
	"context"
	"errors"
	"fmt"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
)

type Rule struct {
	ChatID int64
}

type RuleProvider interface {
	ActiveRules(ctx context.Context) (domain.QualificationMode, []Rule, error)
}

type MemberLookup interface {
	GetChatMember(ctx context.Context, chatID, userID int64) (telegram.ChatMember, error)
}

type IndeterminateReason uint8

const (
	IndeterminateTelegramTemporary IndeterminateReason = 1 << iota
	IndeterminateUnknownMembership
)

type QualificationEvaluation struct {
	Decision            domain.QualificationDecision
	IndeterminateReason IndeterminateReason
}

type Checker struct {
	rules   RuleProvider
	members MemberLookup
}

func NewChecker(rules RuleProvider, members MemberLookup) *Checker {
	return &Checker{rules: rules, members: members}
}

func (checker *Checker) Evaluate(ctx context.Context, telegramID int64) (domain.QualificationDecision, error) {
	evaluation, err := checker.EvaluateDetailed(ctx, telegramID)
	return evaluation.Decision, err
}

func (checker *Checker) EvaluateDetailed(ctx context.Context, telegramID int64) (QualificationEvaluation, error) {
	if telegramID <= 0 {
		return QualificationEvaluation{}, errors.New("Telegram ID must be positive")
	}
	if checker == nil || checker.rules == nil || checker.members == nil {
		return QualificationEvaluation{}, errors.New("qualification checker dependencies are required")
	}
	mode, rules, err := checker.rules.ActiveRules(ctx)
	if err != nil {
		return QualificationEvaluation{}, fmt.Errorf("load qualification rules: %w", err)
	}
	return checker.evaluateRulesDetailed(ctx, telegramID, mode, rules, 0, "", false)
}

func (checker *Checker) EvaluateEvent(
	ctx context.Context,
	event telegram.MembershipEvent,
) (domain.QualificationDecision, bool, error) {
	if event.TelegramID <= 0 || event.ChatID == 0 {
		return "", false, errors.New("membership event identifiers are invalid")
	}
	if checker == nil || checker.rules == nil || checker.members == nil {
		return "", false, errors.New("qualification checker dependencies are required")
	}
	mode, rules, err := checker.rules.ActiveRules(ctx)
	if err != nil {
		return "", false, fmt.Errorf("load qualification rules: %w", err)
	}
	handled := false
	for _, rule := range rules {
		if rule.ChatID == event.ChatID {
			handled = true
			break
		}
	}
	if !handled {
		return "", false, nil
	}
	evaluation, err := checker.evaluateRulesDetailed(ctx, event.TelegramID, mode, rules, event.ChatID, event.Result, true)
	if err != nil {
		return "", true, err
	}
	return evaluation.Decision, true, nil
}

func (checker *Checker) evaluateRulesDetailed(
	ctx context.Context,
	telegramID int64,
	mode domain.QualificationMode,
	rules []Rule,
	observedChatID int64,
	observedResult domain.MembershipResult,
	hasObservation bool,
) (QualificationEvaluation, error) {
	results := make([]domain.MembershipResult, len(rules))
	var indeterminateReason IndeterminateReason
	for index, rule := range rules {
		if rule.ChatID == 0 {
			return QualificationEvaluation{}, errors.New("qualification rule chat ID must not be zero")
		}
		if hasObservation && rule.ChatID == observedChatID {
			switch observedResult {
			case domain.MembershipMember, domain.MembershipNotMember, domain.MembershipIndeterminate:
				results[index] = observedResult
			default:
				return QualificationEvaluation{}, errors.New("membership event result is invalid")
			}
			if observedResult == domain.MembershipIndeterminate {
				indeterminateReason |= IndeterminateUnknownMembership
			}
			continue
		}
		member, err := checker.members.GetChatMember(ctx, rule.ChatID, telegramID)
		if err != nil {
			if telegram.IsTemporary(err) {
				results[index] = domain.MembershipIndeterminate
				indeterminateReason |= IndeterminateTelegramTemporary
				continue
			}
			return QualificationEvaluation{}, fmt.Errorf("query Telegram chat %d membership: %w", rule.ChatID, err)
		}
		results[index] = telegram.MembershipResult(member)
		if results[index] == domain.MembershipIndeterminate {
			indeterminateReason |= IndeterminateUnknownMembership
		}
	}
	decision, err := domain.EvaluateQualification(mode, results)
	if err != nil {
		return QualificationEvaluation{}, fmt.Errorf("evaluate qualification: %w", err)
	}
	if decision != domain.QualificationIndeterminate {
		indeterminateReason = 0
	}
	return QualificationEvaluation{Decision: decision, IndeterminateReason: indeterminateReason}, nil
}
