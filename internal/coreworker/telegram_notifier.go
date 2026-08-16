package coreworker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/postgres"
)

const plannedRestartMessage = "系統即將重啟! 請暫時切換至別的節點"

type VPNRecipientProvider interface {
	ActiveVPNUserIDs(ctx context.Context) ([]int64, error)
}

type CoreAdministratorRecipientProvider interface {
	ActiveAdministratorIDs(ctx context.Context) ([]int64, error)
}

type CoreMessageSender interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
}

type CoreNotificationAuditRecorder interface {
	RecordPlannedRestartNotification(ctx context.Context, attempted, failed int, at time.Time) error
	RecordCoreFailureNotification(ctx context.Context, failure postgres.CoreFailureCode, attempted, failed int, at time.Time) error
}

type TelegramNotifier struct {
	vpnRecipients  VPNRecipientProvider
	administrators CoreAdministratorRecipientProvider
	sender         CoreMessageSender
	audit          CoreNotificationAuditRecorder
	now            func() time.Time
}

func NewTelegramNotifier(
	vpnRecipients VPNRecipientProvider,
	administrators CoreAdministratorRecipientProvider,
	sender CoreMessageSender,
	audit CoreNotificationAuditRecorder,
	now func() time.Time,
) (*TelegramNotifier, error) {
	if vpnRecipients == nil || administrators == nil || sender == nil || audit == nil || now == nil {
		return nil, errors.New("core Telegram notifier dependencies are required")
	}
	return &TelegramNotifier{
		vpnRecipients:  vpnRecipients,
		administrators: administrators,
		sender:         sender,
		audit:          audit,
		now:            now,
	}, nil
}

func (notifier *TelegramNotifier) NotifyPlannedRestart(ctx context.Context) error {
	if notifier == nil || notifier.vpnRecipients == nil || notifier.sender == nil || notifier.audit == nil || notifier.now == nil {
		return errors.New("core Telegram notifier is not configured")
	}
	recipients, err := notifier.vpnRecipients.ActiveVPNUserIDs(ctx)
	if err != nil {
		return fmt.Errorf("list active VPN users: %w", err)
	}
	failed, deliveryErrors := notifier.send(ctx, recipients, plannedRestartMessage)
	at := notifier.now()
	if at.IsZero() {
		deliveryErrors = append(deliveryErrors, errors.New("notification time is invalid"))
	} else if err := notifier.audit.RecordPlannedRestartNotification(ctx, len(recipients), failed, at); err != nil {
		deliveryErrors = append(deliveryErrors, fmt.Errorf("record planned restart notification: %w", err))
	}
	return errors.Join(deliveryErrors...)
}

func (notifier *TelegramNotifier) NotifyCoreFailure(ctx context.Context, failure postgres.CoreFailureCode) error {
	if notifier == nil || notifier.administrators == nil || notifier.sender == nil || notifier.audit == nil || notifier.now == nil {
		return errors.New("core Telegram notifier is not configured")
	}
	if !validFailureCode(failure) {
		return errors.New("core failure code is invalid")
	}
	recipients, err := notifier.administrators.ActiveAdministratorIDs(ctx)
	if err != nil {
		return fmt.Errorf("list active administrators: %w", err)
	}
	message := fmt.Sprintf("VPN 核心更新失敗（%s），請檢查管理面板。", failure)
	failed, deliveryErrors := notifier.send(ctx, recipients, message)
	at := notifier.now()
	if at.IsZero() {
		deliveryErrors = append(deliveryErrors, errors.New("notification time is invalid"))
	} else if err := notifier.audit.RecordCoreFailureNotification(ctx, failure, len(recipients), failed, at); err != nil {
		deliveryErrors = append(deliveryErrors, fmt.Errorf("record core failure notification: %w", err))
	}
	return errors.Join(deliveryErrors...)
}

func (notifier *TelegramNotifier) send(ctx context.Context, recipients []int64, message string) (int, []error) {
	failed := 0
	deliveryErrors := make([]error, 0)
	for _, telegramID := range recipients {
		if telegramID <= 0 {
			failed++
			deliveryErrors = append(deliveryErrors, errors.New("notification recipient is invalid"))
			continue
		}
		if err := notifier.sender.SendMessage(ctx, telegramID, message); err != nil {
			failed++
			deliveryErrors = append(deliveryErrors, fmt.Errorf("send core notification to %d: %w", telegramID, err))
		}
	}
	return failed, deliveryErrors
}

func validFailureCode(failure postgres.CoreFailureCode) bool {
	switch failure {
	case postgres.CoreFailureSnapshot, postgres.CoreFailureCheck, postgres.CoreFailurePromote, postgres.CoreFailureRestart:
		return true
	default:
		return false
	}
}
