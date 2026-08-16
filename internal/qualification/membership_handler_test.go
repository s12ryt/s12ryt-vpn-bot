package qualification

import (
	"context"
	"errors"
	"testing"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
)

func TestMembershipHandlerPersistsExplicitDecision(t *testing.T) {
	evaluator := &eventEvaluatorStub{decision: domain.QualificationIneligible, handled: true}
	writer := &decisionWriterStub{}
	handler := NewMembershipHandler(evaluator, writer)
	event := telegram.MembershipEvent{ChatID: -1001, TelegramID: 12345, Result: domain.MembershipNotMember}

	if err := handler.HandleMembership(context.Background(), event); err != nil {
		t.Fatalf("HandleMembership() error = %v", err)
	}
	if writer.calls != 1 || writer.telegramID != 12345 || writer.decision != domain.QualificationIneligible {
		t.Fatalf("writer = calls:%d telegramID:%d decision:%q", writer.calls, writer.telegramID, writer.decision)
	}
}

func TestMembershipHandlerDoesNotPersistIgnoredOrIndeterminateEvent(t *testing.T) {
	for _, evaluator := range []*eventEvaluatorStub{
		{handled: false},
		{decision: domain.QualificationIndeterminate, handled: true},
	} {
		writer := &decisionWriterStub{}
		handler := NewMembershipHandler(evaluator, writer)
		if err := handler.HandleMembership(context.Background(), telegram.MembershipEvent{ChatID: -1001, TelegramID: 12345}); err != nil {
			t.Fatalf("HandleMembership() error = %v", err)
		}
		if writer.calls != 0 {
			t.Fatalf("writer calls = %d, want zero", writer.calls)
		}
	}
}

func TestMembershipHandlerPropagatesEvaluationAndPersistenceFailures(t *testing.T) {
	evaluateErr := errors.New("qualification unavailable")
	if err := NewMembershipHandler(&eventEvaluatorStub{err: evaluateErr}, &decisionWriterStub{}).
		HandleMembership(context.Background(), telegram.MembershipEvent{TelegramID: 12345}); !errors.Is(err, evaluateErr) {
		t.Fatalf("evaluation error = %v, want %v", err, evaluateErr)
	}

	writeErr := errors.New("database unavailable")
	writer := &decisionWriterStub{err: writeErr}
	err := NewMembershipHandler(&eventEvaluatorStub{decision: domain.QualificationEligible, handled: true}, writer).
		HandleMembership(context.Background(), telegram.MembershipEvent{TelegramID: 12345})
	if !errors.Is(err, writeErr) {
		t.Fatalf("persistence error = %v, want %v", err, writeErr)
	}
}

type eventEvaluatorStub struct {
	decision domain.QualificationDecision
	handled  bool
	err      error
}

func (stub *eventEvaluatorStub) EvaluateEvent(context.Context, telegram.MembershipEvent) (domain.QualificationDecision, bool, error) {
	return stub.decision, stub.handled, stub.err
}

type decisionWriterStub struct {
	calls      int
	telegramID int64
	decision   domain.QualificationDecision
	err        error
}

func (stub *decisionWriterStub) ApplyQualification(_ context.Context, telegramID int64, decision domain.QualificationDecision) (domain.AccessChange, error) {
	stub.calls++
	stub.telegramID = telegramID
	stub.decision = decision
	return domain.AccessChange{}, stub.err
}
