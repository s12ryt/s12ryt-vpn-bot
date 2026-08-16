package telegram

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestApprovalRequestNotifierContinuesAfterIndividualDeliveryFailure(t *testing.T) {
	recipients := &approvalRecipientsStub{ids: []int64{11, 22, 33}}
	sender := &approvalRequestSenderStub{failFor: 22}
	notifier, err := NewApprovalRequestNotifier(recipients, sender)
	if err != nil {
		t.Fatal(err)
	}

	err = notifier.NotifyApprovalRequired(context.Background(), 12345)
	if err == nil {
		t.Fatal("NotifyApprovalRequired() error = nil, want partial delivery error")
	}
	if !reflect.DeepEqual(sender.recipients, []int64{11, 22, 33}) {
		t.Fatalf("recipients = %v", sender.recipients)
	}
	if !reflect.DeepEqual(sender.targets, []int64{12345, 12345, 12345}) {
		t.Fatalf("targets = %v", sender.targets)
	}
}

func TestApprovalRequestNotifierRejectsInvalidTargetBeforeDependencies(t *testing.T) {
	recipients := &approvalRecipientsStub{}
	sender := &approvalRequestSenderStub{}
	notifier, _ := NewApprovalRequestNotifier(recipients, sender)
	if err := notifier.NotifyApprovalRequired(context.Background(), 0); err == nil {
		t.Fatal("NotifyApprovalRequired(0) error = nil")
	}
	if recipients.calls != 0 || len(sender.recipients) != 0 {
		t.Fatal("invalid target reached dependencies")
	}
}

type approvalRecipientsStub struct {
	ids   []int64
	err   error
	calls int
}

func (stub *approvalRecipientsStub) ActiveAdministratorIDs(context.Context) ([]int64, error) {
	stub.calls++
	return stub.ids, stub.err
}

type approvalRequestSenderStub struct {
	recipients []int64
	targets    []int64
	failFor    int64
}

func (stub *approvalRequestSenderStub) SendApprovalRequest(_ context.Context, administratorID, targetTelegramID int64) error {
	stub.recipients = append(stub.recipients, administratorID)
	stub.targets = append(stub.targets, targetTelegramID)
	if administratorID == stub.failFor {
		return errors.New("telegram unavailable")
	}
	return nil
}
