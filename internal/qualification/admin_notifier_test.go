package qualification

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestAdministratorNotifierSendsOneSummaryToEveryActiveAdministrator(t *testing.T) {
	recipients := &administratorRecipientStub{ids: []int64{101, 202}}
	sender := &administratorMessageSenderStub{}
	notifier, err := NewAdministratorNotifier(recipients, sender)
	if err != nil {
		t.Fatalf("NewAdministratorNotifier() error = %v", err)
	}
	summary := RecheckSummary{
		Checked: 60, Eligible: 20, Ineligible: 20, Indeterminate: 20, Revocations: 10,
		TelegramTemporary: 12, UnknownMembership: 7, UnclassifiedIndeterminate: 1,
	}

	if err := notifier.NotifySummary(context.Background(), summary); err != nil {
		t.Fatalf("NotifySummary() error = %v", err)
	}
	if !reflect.DeepEqual(sender.chatIDs, []int64{101, 202}) {
		t.Fatalf("chat IDs = %v", sender.chatIDs)
	}
	if len(sender.messages) != 2 || sender.messages[0] != sender.messages[1] {
		t.Fatalf("messages = %#v", sender.messages)
	}
	for _, expected := range []string{
		"已查核：60", "符合：20", "不符合：20", "未決：20", "撤銷：10",
		"Telegram 暫時錯誤：12", "未知成員狀態：7", "未分類：1",
	} {
		if !strings.Contains(sender.messages[0], expected) {
			t.Fatalf("summary message %q does not contain %q", sender.messages[0], expected)
		}
	}
}

func TestAdministratorNotifierReportsGenericFailureWithoutLeakingCause(t *testing.T) {
	recipients := &administratorRecipientStub{ids: []int64{101}}
	sender := &administratorMessageSenderStub{}
	notifier, err := NewAdministratorNotifier(recipients, sender)
	if err != nil {
		t.Fatalf("NewAdministratorNotifier() error = %v", err)
	}

	if err := notifier.NotifyFailure(context.Background(), errors.New("postgres password=secret")); err != nil {
		t.Fatalf("NotifyFailure() error = %v", err)
	}
	if len(sender.messages) != 1 || strings.Contains(sender.messages[0], "secret") || !strings.Contains(sender.messages[0], "資格補償重查失敗") {
		t.Fatalf("failure message = %#v", sender.messages)
	}
}

func TestAdministratorNotifierContinuesAfterOneDeliveryFailure(t *testing.T) {
	wantErr := errors.New("blocked by recipient")
	recipients := &administratorRecipientStub{ids: []int64{101, 202}}
	sender := &administratorMessageSenderStub{failFor: map[int64]error{101: wantErr}}
	notifier, err := NewAdministratorNotifier(recipients, sender)
	if err != nil {
		t.Fatalf("NewAdministratorNotifier() error = %v", err)
	}

	if err := notifier.NotifySummary(context.Background(), RecheckSummary{}); !errors.Is(err, wantErr) {
		t.Fatalf("NotifySummary() error = %v, want delivery failure", err)
	}
	if !reflect.DeepEqual(sender.chatIDs, []int64{101, 202}) {
		t.Fatalf("delivery stopped early: %v", sender.chatIDs)
	}
}

type administratorRecipientStub struct {
	ids []int64
	err error
}

func (stub *administratorRecipientStub) ActiveAdministratorIDs(context.Context) ([]int64, error) {
	return stub.ids, stub.err
}

type administratorMessageSenderStub struct {
	chatIDs  []int64
	messages []string
	failFor  map[int64]error
}

func (stub *administratorMessageSenderStub) SendMessage(_ context.Context, chatID int64, text string) error {
	stub.chatIDs = append(stub.chatIDs, chatID)
	stub.messages = append(stub.messages, text)
	return stub.failFor[chatID]
}
