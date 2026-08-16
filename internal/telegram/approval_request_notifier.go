package telegram

import (
	"context"
	"errors"
	"fmt"
)

type ApprovalAdministratorProvider interface {
	ActiveAdministratorIDs(context.Context) ([]int64, error)
}

type ApprovalRequestSender interface {
	SendApprovalRequest(ctx context.Context, administratorID, targetTelegramID int64) error
}

type ApprovalRequestNotifier struct {
	recipients ApprovalAdministratorProvider
	sender     ApprovalRequestSender
}

func NewApprovalRequestNotifier(recipients ApprovalAdministratorProvider, sender ApprovalRequestSender) (*ApprovalRequestNotifier, error) {
	if recipients == nil || sender == nil {
		return nil, errors.New("approval request notifier dependencies are required")
	}
	return &ApprovalRequestNotifier{recipients: recipients, sender: sender}, nil
}

func (notifier *ApprovalRequestNotifier) NotifyApprovalRequired(ctx context.Context, telegramID int64) error {
	if notifier == nil || notifier.recipients == nil || notifier.sender == nil || telegramID <= 0 {
		return errors.New("approval request is invalid")
	}
	recipients, err := notifier.recipients.ActiveAdministratorIDs(ctx)
	if err != nil {
		return fmt.Errorf("list approval recipients: %w", err)
	}
	var deliveryErrors []error
	for _, recipient := range recipients {
		if recipient <= 0 {
			return errors.New("approval recipient is invalid")
		}
		if err := notifier.sender.SendApprovalRequest(ctx, recipient, telegramID); err != nil {
			deliveryErrors = append(deliveryErrors, errors.New("deliver approval request"))
		}
	}
	return errors.Join(deliveryErrors...)
}
