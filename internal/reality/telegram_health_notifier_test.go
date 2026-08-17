package reality

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTelegramHealthNotifierContinuesDeliveryAndAuditsFailure(t *testing.T) {
	now := time.Date(2026, 8, 18, 11, 0, 0, 0, time.UTC)
	sender := &realityMessageSenderStub{failFor: map[int64]error{202: errors.New("telegram unavailable")}}
	audit := &realityNotificationAuditStub{}
	notifier, err := NewTelegramHealthNotifier(&realityAdminRecipientsStub{ids: []int64{101, 202, 303}}, sender, audit, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewTelegramHealthNotifier() error = %v", err)
	}

	err = notifier.NotifyRealityFailure(context.Background(), "www.example.com")
	if err != nil {
		t.Fatalf("NotifyRealityFailure() error = %v", err)
	}
	if !reflect.DeepEqual(sender.ids, []int64{101, 202, 303}) {
		t.Fatalf("delivery IDs = %v", sender.ids)
	}
	for _, message := range sender.messages {
		if !strings.Contains(message, "www.example.com") || !strings.Contains(message, "請在管理面板確認後再切換") {
			t.Fatalf("failure message = %q", message)
		}
	}
	if audit.call != (realityAuditCall{target: "www.example.com", healthy: false, attempted: 3, failed: 1, at: now}) {
		t.Fatalf("audit = %#v", audit.call)
	}
}

func TestTelegramHealthNotifierSendsRecoveryOnceRequested(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	sender := &realityMessageSenderStub{}
	audit := &realityNotificationAuditStub{}
	notifier, err := NewTelegramHealthNotifier(&realityAdminRecipientsStub{ids: []int64{101}}, sender, audit, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewTelegramHealthNotifier() error = %v", err)
	}
	if err := notifier.NotifyRealityRecovery(context.Background(), "www.example.com"); err != nil {
		t.Fatalf("NotifyRealityRecovery() error = %v", err)
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "健康檢查已恢復") {
		t.Fatalf("recovery messages = %#v", sender.messages)
	}
	if !audit.call.healthy || audit.call.failed != 0 {
		t.Fatalf("recovery audit = %#v", audit.call)
	}
}

type realityAdminRecipientsStub struct {
	ids []int64
	err error
}

func (stub *realityAdminRecipientsStub) ActiveAdministratorIDs(context.Context) ([]int64, error) {
	return stub.ids, stub.err
}

type realityMessageSenderStub struct {
	ids      []int64
	messages []string
	failFor  map[int64]error
}

func (stub *realityMessageSenderStub) SendMessage(_ context.Context, id int64, message string) error {
	stub.ids = append(stub.ids, id)
	stub.messages = append(stub.messages, message)
	return stub.failFor[id]
}

type realityAuditCall struct {
	target    string
	healthy   bool
	attempted int
	failed    int
	at        time.Time
}

type realityNotificationAuditStub struct {
	call realityAuditCall
}

func (stub *realityNotificationAuditStub) RecordRealityHealthNotification(_ context.Context, target string, healthy bool, attempted, failed int, at time.Time) error {
	stub.call = realityAuditCall{target: target, healthy: healthy, attempted: attempted, failed: failed, at: at}
	return nil
}
