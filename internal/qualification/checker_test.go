package qualification

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
)

func TestCheckerEvaluatesAllRulesAndTreatsTelegramErrorsAsIndeterminate(t *testing.T) {
	rules := &ruleProviderStub{
		mode:  domain.QualificationAll,
		rules: []Rule{{ChatID: -1001}, {ChatID: -1002}},
	}
	members := &memberLookupStub{
		members: map[int64]telegram.ChatMember{
			-1001: {User: telegram.User{ID: 12345}, Status: "member"},
		},
		errors: map[int64]error{-1002: temporaryTelegramError()},
	}
	checker := NewChecker(rules, members)

	decision, err := checker.Evaluate(context.Background(), 12345)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision != domain.QualificationIndeterminate {
		t.Fatalf("Evaluate() = %q, want indeterminate", decision)
	}
	if !reflect.DeepEqual(members.calls, []memberCall{{chatID: -1001, userID: 12345}, {chatID: -1002, userID: 12345}}) {
		t.Fatalf("member calls = %#v", members.calls)
	}
}

func TestCheckerReturnsExplicitIneligibleResultDespiteAnotherTemporaryFailure(t *testing.T) {
	rules := &ruleProviderStub{
		mode:  domain.QualificationAll,
		rules: []Rule{{ChatID: -1001}, {ChatID: -1002}},
	}
	members := &memberLookupStub{
		members: map[int64]telegram.ChatMember{
			-1002: {User: telegram.User{ID: 12345}, Status: "left"},
		},
		errors: map[int64]error{-1001: temporaryTelegramError()},
	}
	checker := NewChecker(rules, members)

	decision, err := checker.Evaluate(context.Background(), 12345)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision != domain.QualificationIneligible {
		t.Fatalf("Evaluate() = %q, want ineligible", decision)
	}
}

func TestCheckerRejectsInvalidUserAndPropagatesRuleStoreFailure(t *testing.T) {
	storeErr := errors.New("rule store unavailable")
	checker := NewChecker(&ruleProviderStub{err: storeErr}, &memberLookupStub{})

	if _, err := checker.Evaluate(context.Background(), 0); err == nil {
		t.Fatal("Evaluate(0) error = nil")
	}
	if _, err := checker.Evaluate(context.Background(), 12345); !errors.Is(err, storeErr) {
		t.Fatalf("Evaluate() error = %v, want rule store error", err)
	}
}

func TestCheckerEvaluatesConfiguredMembershipEventUsingObservedResult(t *testing.T) {
	rules := &ruleProviderStub{
		mode:  domain.QualificationAll,
		rules: []Rule{{ChatID: -1001}, {ChatID: -1002}},
	}
	members := &memberLookupStub{errors: map[int64]error{-1002: temporaryTelegramError()}}
	checker := NewChecker(rules, members)
	event := telegram.MembershipEvent{
		ChatID:     -1001,
		ChatType:   telegram.ChatSupergroup,
		TelegramID: 12345,
		Result:     domain.MembershipNotMember,
	}

	decision, handled, err := checker.EvaluateEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("EvaluateEvent() error = %v", err)
	}
	if !handled || decision != domain.QualificationIneligible {
		t.Fatalf("EvaluateEvent() = (%q, %v), want (ineligible, true)", decision, handled)
	}
	if !reflect.DeepEqual(members.calls, []memberCall{{chatID: -1002, userID: 12345}}) {
		t.Fatalf("member calls = %#v, want only the other configured group", members.calls)
	}
}

func TestCheckerPropagatesPermanentTelegramFailureWithoutProducingDecision(t *testing.T) {
	permanent := &telegram.APIError{StatusCode: 400, ErrorCode: 400}
	checker := NewChecker(
		&ruleProviderStub{mode: domain.QualificationAny, rules: []Rule{{ChatID: -1001}}},
		&memberLookupStub{errors: map[int64]error{-1001: permanent}},
	)

	decision, err := checker.Evaluate(context.Background(), 12345)
	if !errors.Is(err, permanent) {
		t.Fatalf("Evaluate() error = %v, want permanent Telegram failure", err)
	}
	if decision != "" {
		t.Fatalf("Evaluate() decision = %q, want none", decision)
	}
}

func TestCheckerClassifiesIndeterminateReason(t *testing.T) {
	tests := []struct {
		name    string
		members *memberLookupStub
		reason  IndeterminateReason
	}{
		{
			name:    "temporary Telegram failure",
			members: &memberLookupStub{errors: map[int64]error{-1001: temporaryTelegramError()}},
			reason:  IndeterminateTelegramTemporary,
		},
		{
			name: "unknown membership status",
			members: &memberLookupStub{members: map[int64]telegram.ChatMember{
				-1001: {User: telegram.User{ID: 12345}, Status: "future_status"},
			}},
			reason: IndeterminateUnknownMembership,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			checker := NewChecker(
				&ruleProviderStub{mode: domain.QualificationAny, rules: []Rule{{ChatID: -1001}}},
				testCase.members,
			)
			evaluation, err := checker.EvaluateDetailed(context.Background(), 12345)
			if err != nil {
				t.Fatalf("EvaluateDetailed() error = %v", err)
			}
			if evaluation.Decision != domain.QualificationIndeterminate || evaluation.IndeterminateReason != testCase.reason {
				t.Fatalf("EvaluateDetailed() = %#v", evaluation)
			}
		})
	}
}

func temporaryTelegramError() error {
	return &telegram.APIError{StatusCode: 502, ErrorCode: 502, Temporary: true}
}

func TestCheckerIgnoresMembershipEventFromUnconfiguredChat(t *testing.T) {
	rules := &ruleProviderStub{mode: domain.QualificationAny, rules: []Rule{{ChatID: -1001}}}
	members := &memberLookupStub{}
	checker := NewChecker(rules, members)

	decision, handled, err := checker.EvaluateEvent(context.Background(), telegram.MembershipEvent{
		ChatID: -9999, TelegramID: 12345, Result: domain.MembershipMember,
	})
	if err != nil {
		t.Fatalf("EvaluateEvent() error = %v", err)
	}
	if handled || decision != "" || len(members.calls) != 0 {
		t.Fatalf("EvaluateEvent() = (%q, %v), calls=%#v; want ignored", decision, handled, members.calls)
	}
}

type ruleProviderStub struct {
	mode  domain.QualificationMode
	rules []Rule
	err   error
}

func (stub *ruleProviderStub) ActiveRules(context.Context) (domain.QualificationMode, []Rule, error) {
	return stub.mode, stub.rules, stub.err
}

type memberCall struct {
	chatID int64
	userID int64
}

type memberLookupStub struct {
	members map[int64]telegram.ChatMember
	errors  map[int64]error
	calls   []memberCall
}

func (stub *memberLookupStub) GetChatMember(_ context.Context, chatID, userID int64) (telegram.ChatMember, error) {
	stub.calls = append(stub.calls, memberCall{chatID: chatID, userID: userID})
	if err := stub.errors[chatID]; err != nil {
		return telegram.ChatMember{}, err
	}
	return stub.members[chatID], nil
}
