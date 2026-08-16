package trafficrunner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTelegramFaultNotifierSendsClosedFailureAndAuditsCounts(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 5, 0, 0, time.UTC)
	recipients := &faultRecipientsStub{ids: []int64{11, 22}}
	sender := &faultSenderStub{failFor: 22}
	audit := &faultAuditStub{}
	notifier, err := NewTelegramFaultNotifier(recipients, sender, audit, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewTelegramFaultNotifier() error = %v", err)
	}

	err = notifier.NotifyFailure(context.Background(), FaultNotification{Stage: FailureRecord, FailClosed: true, StartedAt: now.Add(-5 * time.Minute)})
	if err == nil {
		t.Fatal("NotifyFailure() error = nil, want delivery error")
	}
	if len(sender.messages) != 2 || !strings.Contains(sender.messages[0], "record") || !strings.Contains(sender.messages[0], "已封閉") {
		t.Fatalf("messages = %#v", sender.messages)
	}
	if audit.failureStage != FailureRecord || audit.attempted != 2 || audit.failed != 1 || !audit.failClosed {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestTelegramFaultNotifierSendsRecoveryWithoutSensitiveDetails(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 6, 0, 0, time.UTC)
	sender := &faultSenderStub{}
	audit := &faultAuditStub{}
	notifier, _ := NewTelegramFaultNotifier(&faultRecipientsStub{ids: []int64{11}}, sender, audit, func() time.Time { return now })
	recovery := FaultRecovery{Recovered: true, WasFailClosed: true, StartedAt: now.Add(-6 * time.Minute)}
	if err := notifier.NotifyRecovery(context.Background(), recovery); err != nil {
		t.Fatalf("NotifyRecovery() error = %v", err)
	}
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "流量計量已恢復") {
		t.Fatalf("messages = %#v", sender.messages)
	}
	if !audit.recovered || !audit.wasFailClosed || audit.attempted != 1 {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestTelegramFaultNotifierRejectsInvalidFailureBeforeSending(t *testing.T) {
	sender := &faultSenderStub{}
	notifier, _ := NewTelegramFaultNotifier(&faultRecipientsStub{}, sender, &faultAuditStub{}, time.Now)
	err := notifier.NotifyFailure(context.Background(), FaultNotification{Stage: FailureStage("password=secret"), StartedAt: time.Now()})
	if err == nil || len(sender.messages) != 0 {
		t.Fatalf("NotifyFailure() error=%v messages=%#v", err, sender.messages)
	}
}

type faultRecipientsStub struct{ ids []int64 }

func (stub *faultRecipientsStub) ActiveAdministratorIDs(context.Context) ([]int64, error) {
	return stub.ids, nil
}

type faultSenderStub struct {
	failFor  int64
	messages []string
}

func (stub *faultSenderStub) SendMessage(_ context.Context, id int64, message string) error {
	stub.messages = append(stub.messages, message)
	if id == stub.failFor {
		return errors.New("telegram secret response")
	}
	return nil
}

type faultAuditStub struct {
	failureStage                         FailureStage
	failClosed, recovered, wasFailClosed bool
	attempted, failed                    int
}

func (stub *faultAuditStub) RecordTrafficFailureNotification(_ context.Context, stage string, failClosed bool, attempted, failed int, _ time.Time) error {
	stub.failureStage, stub.failClosed, stub.attempted, stub.failed = FailureStage(stage), failClosed, attempted, failed
	return nil
}
func (stub *faultAuditStub) RecordTrafficRecoveryNotification(_ context.Context, wasFailClosed bool, attempted, failed int, _ time.Time) error {
	stub.recovered, stub.wasFailClosed, stub.attempted, stub.failed = true, wasFailClosed, attempted, failed
	return nil
}
