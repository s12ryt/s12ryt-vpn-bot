package reality

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type RealityAdministratorRecipients interface {
	ActiveAdministratorIDs(context.Context) ([]int64, error)
}

type RealityMessageSender interface {
	SendMessage(context.Context, int64, string) error
}

type RealityHealthAuditRecorder interface {
	RecordRealityHealthNotification(context.Context, string, bool, int, int, time.Time) error
}

type TelegramHealthNotifier struct {
	recipients RealityAdministratorRecipients
	sender     RealityMessageSender
	audit      RealityHealthAuditRecorder
	now        func() time.Time
}

func NewTelegramHealthNotifier(recipients RealityAdministratorRecipients, sender RealityMessageSender, audit RealityHealthAuditRecorder, now func() time.Time) (*TelegramHealthNotifier, error) {
	if recipients == nil || sender == nil || audit == nil || now == nil {
		return nil, errors.New("REALITY health notifier dependencies are required")
	}
	return &TelegramHealthNotifier{recipients: recipients, sender: sender, audit: audit, now: now}, nil
}

func (notifier *TelegramHealthNotifier) NotifyRealityFailure(ctx context.Context, target string) error {
	message := fmt.Sprintf("REALITY 目標 %s 健康檢查失敗，請在管理面板確認後再切換。系統不會自動切換目標。", target)
	return notifier.notify(ctx, target, false, message)
}

func (notifier *TelegramHealthNotifier) NotifyRealityRecovery(ctx context.Context, target string) error {
	message := fmt.Sprintf("REALITY 目標 %s 健康檢查已恢復。", target)
	return notifier.notify(ctx, target, true, message)
}

func (notifier *TelegramHealthNotifier) notify(ctx context.Context, target string, healthy bool, message string) error {
	if notifier == nil || notifier.recipients == nil || notifier.sender == nil || notifier.audit == nil || notifier.now == nil {
		return errors.New("REALITY health notifier is not configured")
	}
	if err := ValidateTargetDomain(target); err != nil {
		return err
	}
	recipients, err := notifier.recipients.ActiveAdministratorIDs(ctx)
	if err != nil {
		return fmt.Errorf("list REALITY health notification recipients: %w", err)
	}
	failed := 0
	for _, telegramID := range recipients {
		if telegramID <= 0 {
			failed++
			continue
		}
		if err := notifier.sender.SendMessage(ctx, telegramID, message); err != nil {
			failed++
		}
	}
	at := notifier.now().UTC()
	if at.IsZero() {
		return errors.New("REALITY health notification time is invalid")
	}
	if err := notifier.audit.RecordRealityHealthNotification(ctx, target, healthy, len(recipients), failed, at); err != nil {
		return fmt.Errorf("record REALITY health notification audit: %w", err)
	}
	return nil
}
