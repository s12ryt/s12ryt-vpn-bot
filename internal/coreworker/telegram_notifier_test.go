package coreworker

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/postgres"
)

func TestTelegramNotifierSendsPlannedRestartToEveryActiveVPNUserAndAuditsFailures(t *testing.T) {
	now := time.Date(2026, time.August, 17, 10, 0, 0, 0, time.UTC)
	wantDeliveryErr := errors.New("telegram response contained secret")
	sender := &coreMessageSenderStub{failFor: map[int64]error{202: wantDeliveryErr}}
	audit := &coreNotificationAuditStub{}
	notifier, err := NewTelegramNotifier(
		&coreVPNRecipientStub{ids: []int64{101, 202, 303}},
		&coreAdministratorRecipientStub{ids: []int64{9001}},
		sender,
		audit,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewTelegramNotifier() error = %v", err)
	}

	err = notifier.NotifyPlannedRestart(context.Background())
	if !errors.Is(err, wantDeliveryErr) {
		t.Fatalf("NotifyPlannedRestart() error = %v, want delivery error", err)
	}
	if !reflect.DeepEqual(sender.chatIDs, []int64{101, 202, 303}) {
		t.Fatalf("delivery stopped early: %v", sender.chatIDs)
	}
	const message = "系統即將重啟! 請暫時切換至別的節點"
	if !reflect.DeepEqual(sender.messages, []string{message, message, message}) {
		t.Fatalf("messages = %#v", sender.messages)
	}
	if audit.planned != (coreNotificationAuditCall{attempted: 3, failed: 1, at: now}) {
		t.Fatalf("planned audit = %#v", audit.planned)
	}
}

func TestTelegramNotifierSendsOnlyClosedCoreFailureCodeToAdministrators(t *testing.T) {
	now := time.Date(2026, time.August, 17, 10, 0, 0, 0, time.UTC)
	sender := &coreMessageSenderStub{}
	audit := &coreNotificationAuditStub{}
	notifier, err := NewTelegramNotifier(
		&coreVPNRecipientStub{},
		&coreAdministratorRecipientStub{ids: []int64{9001, 9002}},
		sender,
		audit,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewTelegramNotifier() error = %v", err)
	}

	if err := notifier.NotifyCoreFailure(context.Background(), postgres.CoreFailureRestart); err != nil {
		t.Fatalf("NotifyCoreFailure() error = %v", err)
	}
	if !reflect.DeepEqual(sender.chatIDs, []int64{9001, 9002}) {
		t.Fatalf("administrator chat IDs = %v", sender.chatIDs)
	}
	for _, message := range sender.messages {
		if message != "VPN 核心更新失敗（restart），請檢查管理面板。" || strings.Contains(message, "secret") {
			t.Fatalf("failure message = %q", message)
		}
	}
	if audit.failure != (coreFailureAuditCall{failure: postgres.CoreFailureRestart, attempted: 2, failed: 0, at: now}) {
		t.Fatalf("failure audit = %#v", audit.failure)
	}
}

func TestTelegramNotifierRejectsInvalidFailureWithoutSendingOrAuditing(t *testing.T) {
	sender := &coreMessageSenderStub{}
	audit := &coreNotificationAuditStub{}
	notifier, err := NewTelegramNotifier(
		&coreVPNRecipientStub{},
		&coreAdministratorRecipientStub{ids: []int64{9001}},
		sender,
		audit,
		time.Now,
	)
	if err != nil {
		t.Fatalf("NewTelegramNotifier() error = %v", err)
	}

	if err := notifier.NotifyCoreFailure(context.Background(), postgres.CoreFailureCode("database password=secret")); err == nil {
		t.Fatal("NotifyCoreFailure(invalid) error = nil")
	}
	if len(sender.chatIDs) != 0 || audit.failure.at != (time.Time{}) {
		t.Fatal("invalid failure reached sender or audit")
	}
}

type coreVPNRecipientStub struct {
	ids []int64
	err error
}

func (stub *coreVPNRecipientStub) ActiveVPNUserIDs(context.Context) ([]int64, error) {
	return stub.ids, stub.err
}

type coreAdministratorRecipientStub struct {
	ids []int64
	err error
}

func (stub *coreAdministratorRecipientStub) ActiveAdministratorIDs(context.Context) ([]int64, error) {
	return stub.ids, stub.err
}

type coreMessageSenderStub struct {
	chatIDs  []int64
	messages []string
	failFor  map[int64]error
}

func (stub *coreMessageSenderStub) SendMessage(_ context.Context, chatID int64, message string) error {
	stub.chatIDs = append(stub.chatIDs, chatID)
	stub.messages = append(stub.messages, message)
	return stub.failFor[chatID]
}

type coreNotificationAuditCall struct {
	attempted int
	failed    int
	at        time.Time
}

type coreFailureAuditCall struct {
	failure   postgres.CoreFailureCode
	attempted int
	failed    int
	at        time.Time
}

type coreNotificationAuditStub struct {
	planned coreNotificationAuditCall
	failure coreFailureAuditCall
}

func (stub *coreNotificationAuditStub) RecordPlannedRestartNotification(_ context.Context, attempted, failed int, at time.Time) error {
	stub.planned = coreNotificationAuditCall{attempted: attempted, failed: failed, at: at}
	return nil
}

func (stub *coreNotificationAuditStub) RecordCoreFailureNotification(_ context.Context, failure postgres.CoreFailureCode, attempted, failed int, at time.Time) error {
	stub.failure = coreFailureAuditCall{failure: failure, attempted: attempted, failed: failed, at: at}
	return nil
}
