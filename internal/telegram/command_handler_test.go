package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/vpn"
)

func TestAdminLoginCommandReturnsOnlyCodeInPrivateChat(t *testing.T) {
	issuer := &fakeLoginCodeIssuer{code: "Ab12Cd34"}
	handler := NewCommandHandler("vpn_test_bot", issuer)

	reply, handled := handler.Handle(context.Background(), Message{
		ChatType: ChatPrivate,
		SenderID: 12345,
		Text:     "/adminlogin",
	})
	if !handled {
		t.Fatal("Handle() handled = false, want true")
	}
	if reply.Text != "Ab12Cd34" {
		t.Fatalf("Handle() reply = %q, want code only", reply.Text)
	}
	if issuer.calls != 1 || issuer.telegramID != 12345 {
		t.Fatalf("issuer calls = %d, telegramID = %d", issuer.calls, issuer.telegramID)
	}
}

func TestAdminLoginCommandRateLimitUsesGenericFailureAndSkipsIssuer(t *testing.T) {
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	issuer := &fakeLoginCodeIssuer{code: "Ab12Cd34"}
	handler := NewCommandHandler("vpn_test_bot", issuer, NewAdminLoginRateLimiter(func() time.Time { return now }))
	message := Message{ChatType: ChatPrivate, SenderID: 12345, Text: "/adminlogin"}

	for attempt := 1; attempt <= 3; attempt++ {
		if reply, handled := handler.Handle(context.Background(), message); !handled || reply.Text != "Ab12Cd34" {
			t.Fatalf("Handle() attempt %d = (%#v, %v), want code", attempt, reply, handled)
		}
	}
	reply, handled := handler.Handle(context.Background(), message)
	if !handled || reply.Text != "無法產生登入碼。" {
		t.Fatalf("rate-limited Handle() = (%#v, %v), want generic failure", reply, handled)
	}
	if issuer.calls != 3 {
		t.Fatalf("issuer calls = %d, want 3", issuer.calls)
	}
}

func TestAdminLoginCommandAcceptsOwnBotUsernameSuffix(t *testing.T) {
	issuer := &fakeLoginCodeIssuer{code: "Ab12Cd34"}
	handler := NewCommandHandler("vpn_test_bot", issuer)

	reply, handled := handler.Handle(context.Background(), Message{
		ChatType: ChatPrivate,
		SenderID: 12345,
		Text:     "/adminlogin@vpn_test_bot",
	})
	if !handled || reply.Text != "Ab12Cd34" {
		t.Fatalf("Handle() = (%#v, %v), want code reply", reply, handled)
	}
}

func TestAdminLoginCommandNeverIssuesCodeInGroup(t *testing.T) {
	issuer := &fakeLoginCodeIssuer{code: "Ab12Cd34"}
	handler := NewCommandHandler("vpn_test_bot", issuer)

	reply, handled := handler.Handle(context.Background(), Message{
		ChatType: ChatSupergroup,
		SenderID: 12345,
		Text:     "/adminlogin",
	})
	if !handled {
		t.Fatal("Handle() handled = false, want safe rejection")
	}
	if reply.Text != "此指令只能在 Bot 私聊使用。" {
		t.Fatalf("Handle() reply = %q", reply.Text)
	}
	if issuer.calls != 0 {
		t.Fatalf("issuer calls = %d, want 0", issuer.calls)
	}
}

func TestAdminLoginCommandUsesGenericUnauthorizedReply(t *testing.T) {
	issuer := &fakeLoginCodeIssuer{err: errors.New("not authorized")}
	handler := NewCommandHandler("vpn_test_bot", issuer)

	reply, handled := handler.Handle(context.Background(), Message{
		ChatType: ChatPrivate,
		SenderID: 99999,
		Text:     "/adminlogin",
	})
	if !handled || reply.Text != "無法產生登入碼。" {
		t.Fatalf("Handle() = (%#v, %v), want generic error", reply, handled)
	}
}

func TestCommandHandlerIgnoresOtherCommandsAndForeignSuffix(t *testing.T) {
	issuer := &fakeLoginCodeIssuer{code: "Ab12Cd34"}
	handler := NewCommandHandler("vpn_test_bot", issuer)

	for _, text := range []string{"/unknown", "/adminlogin@another_bot", "/vpn@another_bot", "adminlogin"} {
		if reply, handled := handler.Handle(context.Background(), Message{ChatType: ChatPrivate, SenderID: 12345, Text: text}); handled {
			t.Errorf("Handle(%q) = (%#v, true), want ignored", text, reply)
		}
	}
	if issuer.calls != 0 {
		t.Fatalf("issuer calls = %d, want 0", issuer.calls)
	}
}

func TestVPNCommandReturnsPrivateSubscriptionAfterExplicitQualification(t *testing.T) {
	provider := &fakeVPNAccessProvider{access: vpn.Access{
		SubscriptionURL: "https://vpn.example.com/sub/private-token",
		NewlyIssued:     true,
	}}
	handler := NewCommandHandler("vpn_test_bot", &fakeLoginCodeIssuer{}).WithVPNAccess(provider)

	reply, handled := handler.Handle(context.Background(), Message{ChatType: ChatPrivate, SenderID: 12345, Text: "/vpn@vpn_test_bot"})
	if !handled || !strings.Contains(reply.Text, provider.access.SubscriptionURL) || !strings.Contains(reply.Text, "建立") {
		t.Fatalf("Handle(/vpn) = (%#v, %v)", reply, handled)
	}
	if provider.telegramID != 12345 || provider.calls != 1 {
		t.Fatalf("provider ID=%d calls=%d", provider.telegramID, provider.calls)
	}
}

func TestVPNCommandNeverRevealsSubscriptionInGroup(t *testing.T) {
	provider := &fakeVPNAccessProvider{access: vpn.Access{SubscriptionURL: "https://vpn.example.com/sub/private-token"}}
	handler := NewCommandHandler("vpn_test_bot", &fakeLoginCodeIssuer{}).WithVPNAccess(provider)

	reply, handled := handler.Handle(context.Background(), Message{ChatType: ChatSupergroup, SenderID: 12345, Text: "/vpn"})
	if !handled || reply.Text != "此指令只能在 Bot 私聊使用。" || provider.calls != 0 {
		t.Fatalf("group Handle(/vpn) = (%#v, %v), provider calls=%d", reply, handled, provider.calls)
	}
}

func TestVPNCommandMapsExpectedFailuresWithoutLeakingInternalErrors(t *testing.T) {
	tests := []struct {
		err      error
		wantText string
	}{
		{err: domain.ErrNotEligible, wantText: "目前不符合領取資格。"},
		{err: domain.ErrApprovalRequired, wantText: "重新加入後需等待管理員核准。"},
		{err: vpn.ErrQualificationUnavailable, wantText: "暫時無法驗證資格，請稍後再試。"},
		{err: errors.New("database password=secret"), wantText: "無法取得 VPN，請稍後再試。"},
	}
	for _, testCase := range tests {
		handler := NewCommandHandler("vpn_test_bot", &fakeLoginCodeIssuer{}).WithVPNAccess(&fakeVPNAccessProvider{err: testCase.err})
		reply, handled := handler.Handle(context.Background(), Message{ChatType: ChatPrivate, SenderID: 12345, Text: "/vpn"})
		if !handled || reply.Text != testCase.wantText {
			t.Fatalf("Handle(/vpn) error %v = (%#v, %v)", testCase.err, reply, handled)
		}
	}
}

func TestVPNCommandNotifiesAdministratorsWhenApprovalIsRequired(t *testing.T) {
	notifier := &approvalRequiredNotifierStub{}
	handler := NewCommandHandler("vpn_test_bot", &fakeLoginCodeIssuer{}).
		WithVPNAccess(&fakeVPNAccessProvider{err: domain.ErrApprovalRequired}).
		WithApprovalRequests(notifier)

	reply, handled := handler.Handle(context.Background(), Message{ChatType: ChatPrivate, SenderID: 12345, Text: "/vpn"})
	if !handled || reply.Text != "重新加入後需等待管理員核准。" {
		t.Fatalf("Handle(/vpn) = (%#v, %v)", reply, handled)
	}
	if notifier.telegramID != 12345 || notifier.calls != 1 {
		t.Fatalf("notifier = ID %d calls %d", notifier.telegramID, notifier.calls)
	}
}

func TestStatusCommandReturnsQuotaResetAndPrivateLinkOnlyInPrivateChat(t *testing.T) {
	resetsAt := time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC)
	provider := &statusProviderStub{status: vpn.Status{
		Overview: domain.UserOverview{
			TelegramID: 12345, Eligible: true, Status: domain.AccessStatusActive,
			UsedBytes: 25_000_000_000, LimitBytes: 50_000_000_000,
		},
		SubscriptionURL: "https://vpn.example.com/sub/private",
		ResetsAt:        resetsAt,
	}}
	handler := NewCommandHandler("vpn_test_bot", &fakeLoginCodeIssuer{}).WithStatus(provider)

	reply, handled := handler.Handle(context.Background(), Message{ChatType: ChatPrivate, SenderID: 12345, Text: "/status@vpn_test_bot"})
	if !handled || !strings.Contains(reply.Text, "25.00 GB / 50.00 GB") ||
		!strings.Contains(reply.Text, "2026-08-31 00:00 UTC") || reply.QRContent != provider.status.SubscriptionURL {
		t.Fatalf("Handle(/status) = (%#v, %v)", reply, handled)
	}
	if provider.calls != 1 || provider.telegramID != 12345 {
		t.Fatalf("provider calls=%d telegramID=%d", provider.calls, provider.telegramID)
	}

	reply, handled = handler.Handle(context.Background(), Message{ChatType: ChatSupergroup, SenderID: 12345, Text: "/status"})
	if !handled || reply.Text != "此指令只能在 Bot 私聊使用。" || provider.calls != 1 {
		t.Fatalf("group Handle(/status) = (%#v, %v), calls=%d", reply, handled, provider.calls)
	}
}

func TestStatusCommandUsesOpaqueFailure(t *testing.T) {
	handler := NewCommandHandler("vpn_test_bot", &fakeLoginCodeIssuer{}).
		WithStatus(&statusProviderStub{err: errors.New("database password=secret")})
	reply, handled := handler.Handle(context.Background(), Message{ChatType: ChatPrivate, SenderID: 12345, Text: "/status"})
	if !handled || reply.Text != "無法取得 VPN 狀態，請稍後再試。" || reply.QRContent != "" {
		t.Fatalf("Handle(/status) = (%#v, %v)", reply, handled)
	}
}

func TestAdminCommandsArePrivateAndUseOpaqueFailure(t *testing.T) {
	commands := &adminCommandProviderStub{reply: "管理結果"}
	handler := NewCommandHandler("vpn_test_bot", &fakeLoginCodeIssuer{}).WithAdminCommands(commands)
	reply, handled := handler.Handle(context.Background(), Message{ChatType: ChatPrivate, SenderID: 9001, Text: "/adminstats@vpn_test_bot"})
	if !handled || reply.Text != "管理結果" || commands.calls != 1 {
		t.Fatalf("private admin command = (%#v, %v), calls=%d", reply, handled, commands.calls)
	}
	reply, handled = handler.Handle(context.Background(), Message{ChatType: ChatSupergroup, SenderID: 9001, Text: "/adminstats"})
	if !handled || reply.Text != "此指令只能在 Bot 私聊使用。" || commands.calls != 1 {
		t.Fatalf("group admin command = (%#v, %v), calls=%d", reply, handled, commands.calls)
	}
	commands.err = errors.New("database secret")
	reply, handled = handler.Handle(context.Background(), Message{ChatType: ChatPrivate, SenderID: 9001, Text: "/adminusers"})
	if !handled || reply.Text != "無法執行管理指令。" {
		t.Fatalf("failed admin command = (%#v, %v)", reply, handled)
	}
}

type fakeLoginCodeIssuer struct {
	code       string
	err        error
	calls      int
	telegramID int64
}

type fakeVPNAccessProvider struct {
	access     vpn.Access
	err        error
	telegramID int64
	calls      int
}

type approvalRequiredNotifierStub struct {
	telegramID int64
	calls      int
}

type statusProviderStub struct {
	status     vpn.Status
	err        error
	telegramID int64
	calls      int
}

type adminCommandProviderStub struct {
	reply string
	err   error
	calls int
}

func (stub *adminCommandProviderStub) Execute(context.Context, int64, string) (string, error) {
	stub.calls++
	return stub.reply, stub.err
}

func (stub *statusProviderStub) GetStatus(_ context.Context, telegramID int64) (vpn.Status, error) {
	stub.calls++
	stub.telegramID = telegramID
	return stub.status, stub.err
}

func (stub *approvalRequiredNotifierStub) NotifyApprovalRequired(_ context.Context, telegramID int64) error {
	stub.calls++
	stub.telegramID = telegramID
	return errors.New("delivery failure must not change user reply")
}

func (provider *fakeVPNAccessProvider) GetOrClaim(_ context.Context, telegramID int64) (vpn.Access, error) {
	provider.calls++
	provider.telegramID = telegramID
	return provider.access, provider.err
}

func (issuer *fakeLoginCodeIssuer) Issue(_ context.Context, telegramID int64) (string, error) {
	issuer.calls++
	issuer.telegramID = telegramID
	return issuer.code, issuer.err
}
