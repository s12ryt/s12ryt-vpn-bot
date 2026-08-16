package qualification

import (
	"context"
	"errors"
	"fmt"
)

type AdministratorRecipientProvider interface {
	ActiveAdministratorIDs(ctx context.Context) ([]int64, error)
}

type AdministratorMessageSender interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
}

type AdministratorNotifier struct {
	recipients AdministratorRecipientProvider
	sender     AdministratorMessageSender
}

func NewAdministratorNotifier(recipients AdministratorRecipientProvider, sender AdministratorMessageSender) (*AdministratorNotifier, error) {
	if recipients == nil || sender == nil {
		return nil, errors.New("administrator notifier dependencies are required")
	}
	return &AdministratorNotifier{recipients: recipients, sender: sender}, nil
}

func (notifier *AdministratorNotifier) NotifySummary(ctx context.Context, summary RecheckSummary) error {
	message := fmt.Sprintf(
		"資格補償重查完成\n已查核：%d\n符合：%d\n不符合：%d\n未決：%d\n撤銷：%d\nTelegram 暫時錯誤：%d\n未知成員狀態：%d\n未分類：%d",
		summary.Checked,
		summary.Eligible,
		summary.Ineligible,
		summary.Indeterminate,
		summary.Revocations,
		summary.TelegramTemporary,
		summary.UnknownMembership,
		summary.UnclassifiedIndeterminate,
	)
	return notifier.notify(ctx, message)
}

func (notifier *AdministratorNotifier) NotifyFailure(ctx context.Context, _ error) error {
	return notifier.notify(ctx, "資格補償重查失敗，請檢查服務日誌。")
}

func (notifier *AdministratorNotifier) notify(ctx context.Context, message string) error {
	if notifier == nil || notifier.recipients == nil || notifier.sender == nil {
		return errors.New("administrator notifier is not configured")
	}
	recipients, err := notifier.recipients.ActiveAdministratorIDs(ctx)
	if err != nil {
		return fmt.Errorf("list active administrators: %w", err)
	}
	var deliveryErrors []error
	for _, telegramID := range recipients {
		if telegramID <= 0 {
			deliveryErrors = append(deliveryErrors, errors.New("active administrator Telegram ID is invalid"))
			continue
		}
		if err := notifier.sender.SendMessage(ctx, telegramID, message); err != nil {
			deliveryErrors = append(deliveryErrors, fmt.Errorf("notify administrator %d: %w", telegramID, err))
		}
	}
	return errors.Join(deliveryErrors...)
}
