package trafficrunner

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type FaultAdministratorProvider interface {
	ActiveAdministratorIDs(context.Context) ([]int64, error)
}

type FaultMessageSender interface {
	SendMessage(context.Context, int64, string) error
}

type FaultAuditRecorder interface {
	RecordTrafficFailureNotification(context.Context, string, bool, int, int, time.Time) error
	RecordTrafficRecoveryNotification(context.Context, bool, int, int, time.Time) error
}

type TelegramFaultNotifier struct {
	recipients FaultAdministratorProvider
	sender     FaultMessageSender
	audit      FaultAuditRecorder
	now        func() time.Time
}

func NewTelegramFaultNotifier(recipients FaultAdministratorProvider, sender FaultMessageSender, audit FaultAuditRecorder, now func() time.Time) (*TelegramFaultNotifier, error) {
	if recipients == nil || sender == nil || audit == nil || now == nil {
		return nil, errors.New("traffic notifier dependencies are required")
	}
	return &TelegramFaultNotifier{recipients: recipients, sender: sender, audit: audit, now: now}, nil
}

func (notifier *TelegramFaultNotifier) NotifyFailure(ctx context.Context, notification FaultNotification) error {
	if notifier == nil || !validFailureStage(notification.Stage) || notification.StartedAt.IsZero() {
		return errors.New("traffic failure notification is invalid")
	}
	state := "監控中"
	if notification.FailClosed {
		state = "已封閉 VPN 存取"
	}
	message := fmt.Sprintf("流量計量故障（%s），%s，請檢查管理面板。", notification.Stage, state)
	return notifier.sendAndAudit(ctx, message, func(attempted, failed int, at time.Time) error {
		return notifier.audit.RecordTrafficFailureNotification(ctx, string(notification.Stage), notification.FailClosed, attempted, failed, at)
	})
}

func (notifier *TelegramFaultNotifier) NotifyRecovery(ctx context.Context, recovery FaultRecovery) error {
	if notifier == nil || !recovery.Recovered || recovery.StartedAt.IsZero() {
		return errors.New("traffic recovery notification is invalid")
	}
	return notifier.sendAndAudit(ctx, "流量計量已恢復，累積流量已完成補帳。", func(attempted, failed int, at time.Time) error {
		return notifier.audit.RecordTrafficRecoveryNotification(ctx, recovery.WasFailClosed, attempted, failed, at)
	})
}

func (notifier *TelegramFaultNotifier) sendAndAudit(ctx context.Context, message string, record func(int, int, time.Time) error) error {
	ids, err := notifier.recipients.ActiveAdministratorIDs(ctx)
	if err != nil {
		return errors.New("list traffic notification recipients failed")
	}
	failed := 0
	for _, id := range ids {
		if id <= 0 || notifier.sender.SendMessage(ctx, id, message) != nil {
			failed++
		}
	}
	at := notifier.now()
	if at.IsZero() {
		return errors.New("traffic notification time is invalid")
	}
	auditErr := record(len(ids), failed, at)
	if failed > 0 {
		return errors.Join(errors.New("one or more traffic notifications failed"), auditErr)
	}
	return auditErr
}
