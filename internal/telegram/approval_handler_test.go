package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

func TestApprovalHandlerAuthorizesAdministratorAndApprovesPendingUser(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	admins := approvalAdminStub{administrator: auth.Administrator{TelegramID: 77, Role: auth.RoleAdministrator, Active: true}}
	provisioner := &approvalProvisionerStub{access: domain.ProvisionedAccess{Credentials: domain.CredentialBundle{SubscriptionToken: "token"}}}
	decisions := &approvalDecisionStub{}
	sender := &approvalSenderStub{}
	handler, err := NewApprovalHandler(admins, provisioner, decisions, sender, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewApprovalHandler() error = %v", err)
	}

	err = handler.HandleCallback(context.Background(), Callback{ID: "cb-1", SenderID: 77, Data: "approve:12345"})
	if err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if provisioner.telegramID != 12345 || !provisioner.at.Equal(now) {
		t.Fatalf("Approve args = %d %v", provisioner.telegramID, provisioner.at)
	}
	if len(sender.messages) != 1 || sender.messages[0].telegramID != 12345 || sender.messages[0].text != "你的 VPN 申請已核准，請使用 /vpn 取得私人訂閱連結。" {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestApprovalHandlerRejectsPendingUser(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	decisions := &approvalDecisionStub{}
	sender := &approvalSenderStub{}
	handler, err := NewApprovalHandler(
		approvalAdminStub{administrator: auth.Administrator{TelegramID: 77, Role: auth.RoleAdministrator, Active: true}},
		&approvalProvisionerStub{}, decisions, sender, func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewApprovalHandler() error = %v", err)
	}
	if err := handler.HandleCallback(context.Background(), Callback{ID: "cb-2", SenderID: 77, Data: "reject:12345"}); err != nil {
		t.Fatalf("HandleCallback() error = %v", err)
	}
	if decisions.actorID != 77 || decisions.telegramID != 12345 || !decisions.at.Equal(now) {
		t.Fatalf("reject args = actor %d target %d at %v", decisions.actorID, decisions.telegramID, decisions.at)
	}
	if len(sender.messages) != 1 || sender.messages[0].telegramID != 12345 || sender.messages[0].text != "你的 VPN 申請未獲核准。" {
		t.Fatalf("messages = %#v", sender.messages)
	}
}

func TestApprovalHandlerRejectsUnauthorizedOrMalformedCallbackBeforeProvisioning(t *testing.T) {
	tests := []struct {
		name     string
		callback Callback
		admin    auth.Administrator
		adminErr error
	}{
		{name: "unauthorized", callback: Callback{ID: "cb", SenderID: 88, Data: "approve:12345"}, adminErr: auth.ErrAdministratorUnauthorized},
		{name: "malformed", callback: Callback{ID: "cb", SenderID: 77, Data: "approve:not-a-number"}, admin: auth.Administrator{TelegramID: 77, Role: auth.RoleOwner, Active: true}},
		{name: "missing id", callback: Callback{SenderID: 77, Data: "approve:12345"}, admin: auth.Administrator{TelegramID: 77, Role: auth.RoleOwner, Active: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provisioner := &approvalProvisionerStub{}
			handler, _ := NewApprovalHandler(approvalAdminStub{administrator: test.admin, err: test.adminErr}, provisioner, &approvalDecisionStub{}, &approvalSenderStub{}, time.Now)
			if err := handler.HandleCallback(context.Background(), test.callback); err == nil {
				t.Fatal("HandleCallback() error = nil")
			}
			if provisioner.calls != 0 {
				t.Fatalf("Approve calls = %d", provisioner.calls)
			}
		})
	}
}

type approvalDecisionStub struct {
	actorID    int64
	telegramID int64
	at         time.Time
	err        error
}

func (stub *approvalDecisionStub) RejectApproval(_ context.Context, actorID, telegramID int64, at time.Time) error {
	stub.actorID, stub.telegramID, stub.at = actorID, telegramID, at
	return stub.err
}

type approvalAdminStub struct {
	administrator auth.Administrator
	err           error
}

func (stub approvalAdminStub) FindActive(context.Context, int64) (auth.Administrator, error) {
	return stub.administrator, stub.err
}

type approvalProvisionerStub struct {
	telegramID int64
	at         time.Time
	access     domain.ProvisionedAccess
	err        error
	calls      int
}

func (stub *approvalProvisionerStub) Approve(_ context.Context, id int64, at time.Time) (domain.ProvisionedAccess, error) {
	stub.calls++
	stub.telegramID = id
	stub.at = at
	return stub.access, stub.err
}

type approvalMessage struct {
	telegramID int64
	text       string
}
type approvalSenderStub struct {
	messages []approvalMessage
	err      error
}

func (stub *approvalSenderStub) SendMessage(_ context.Context, id int64, text string) error {
	stub.messages = append(stub.messages, approvalMessage{id, text})
	return stub.err
}

var _ = errors.New
