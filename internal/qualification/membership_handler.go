package qualification

import (
	"context"
	"errors"
	"fmt"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
)

type EventEvaluator interface {
	EvaluateEvent(ctx context.Context, event telegram.MembershipEvent) (domain.QualificationDecision, bool, error)
}

type DecisionWriter interface {
	ApplyQualification(ctx context.Context, telegramID int64, decision domain.QualificationDecision) (domain.AccessChange, error)
}

type MembershipHandler struct {
	evaluator EventEvaluator
	writer    DecisionWriter
}

func NewMembershipHandler(evaluator EventEvaluator, writer DecisionWriter) *MembershipHandler {
	return &MembershipHandler{evaluator: evaluator, writer: writer}
}

func (handler *MembershipHandler) HandleMembership(ctx context.Context, event telegram.MembershipEvent) error {
	if handler == nil || handler.evaluator == nil || handler.writer == nil {
		return errors.New("membership handler dependencies are required")
	}
	decision, handled, err := handler.evaluator.EvaluateEvent(ctx, event)
	if err != nil {
		return fmt.Errorf("evaluate membership event: %w", err)
	}
	if !handled || decision == domain.QualificationIndeterminate {
		return nil
	}
	if _, err := handler.writer.ApplyQualification(ctx, event.TelegramID, decision); err != nil {
		return fmt.Errorf("persist qualification decision: %w", err)
	}
	return nil
}
