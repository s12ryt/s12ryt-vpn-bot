package telegram

import (
	"context"
	"errors"
	"strings"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/vpn"
)

type ChatType string

const (
	ChatPrivate    ChatType = "private"
	ChatGroup      ChatType = "group"
	ChatSupergroup ChatType = "supergroup"
	ChatChannel    ChatType = "channel"
)

type Message struct {
	ChatType ChatType
	SenderID int64
	Text     string
}

type Reply struct {
	Text string
}

type LoginCodeIssuer interface {
	Issue(ctx context.Context, telegramID int64) (string, error)
}

type LoginCodeRateLimiter interface {
	Allow(telegramID int64) bool
}

type VPNAccessProvider interface {
	GetOrClaim(ctx context.Context, telegramID int64) (vpn.Access, error)
}

type ApprovalRequiredNotifier interface {
	NotifyApprovalRequired(ctx context.Context, telegramID int64) error
}

type CommandHandler struct {
	botUsername  string
	loginCodes   LoginCodeIssuer
	loginLimiter LoginCodeRateLimiter
	vpnAccess    VPNAccessProvider
	approvals    ApprovalRequiredNotifier
}

func (handler CommandHandler) WithApprovalRequests(notifier ApprovalRequiredNotifier) CommandHandler {
	handler.approvals = notifier
	return handler
}

func NewCommandHandler(botUsername string, loginCodes LoginCodeIssuer, limiters ...LoginCodeRateLimiter) CommandHandler {
	handler := CommandHandler{
		botUsername: strings.TrimPrefix(botUsername, "@"),
		loginCodes:  loginCodes,
	}
	if len(limiters) > 0 {
		handler.loginLimiter = limiters[0]
	}
	return handler
}

func (handler CommandHandler) WithVPNAccess(provider VPNAccessProvider) CommandHandler {
	handler.vpnAccess = provider
	return handler
}

func (handler CommandHandler) Handle(ctx context.Context, message Message) (Reply, bool) {
	if handler.isAdminLoginCommand(message.Text) {
		return handler.handleAdminLogin(ctx, message)
	}
	if handler.isVPNCommand(message.Text) {
		return handler.handleVPN(ctx, message)
	}
	return Reply{}, false
}

func (handler CommandHandler) handleAdminLogin(ctx context.Context, message Message) (Reply, bool) {
	if message.ChatType != ChatPrivate {
		return Reply{Text: "此指令只能在 Bot 私聊使用。"}, true
	}
	if handler.loginCodes == nil {
		return Reply{Text: "無法產生登入碼。"}, true
	}
	if handler.loginLimiter != nil && !handler.loginLimiter.Allow(message.SenderID) {
		return Reply{Text: "無法產生登入碼。"}, true
	}
	code, err := handler.loginCodes.Issue(ctx, message.SenderID)
	if err != nil {
		return Reply{Text: "無法產生登入碼。"}, true
	}
	return Reply{Text: code}, true
}

func (handler CommandHandler) handleVPN(ctx context.Context, message Message) (Reply, bool) {
	if message.ChatType != ChatPrivate {
		return Reply{Text: "此指令只能在 Bot 私聊使用。"}, true
	}
	if handler.vpnAccess == nil {
		return Reply{Text: "無法取得 VPN，請稍後再試。"}, true
	}
	access, err := handler.vpnAccess.GetOrClaim(ctx, message.SenderID)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrNotEligible):
			return Reply{Text: "目前不符合領取資格。"}, true
		case errors.Is(err, domain.ErrApprovalRequired):
			if handler.approvals != nil {
				_ = handler.approvals.NotifyApprovalRequired(context.WithoutCancel(ctx), message.SenderID)
			}
			return Reply{Text: "重新加入後需等待管理員核准。"}, true
		case errors.Is(err, vpn.ErrQualificationUnavailable):
			return Reply{Text: "暫時無法驗證資格，請稍後再試。"}, true
		default:
			return Reply{Text: "無法取得 VPN，請稍後再試。"}, true
		}
	}
	if access.NewlyIssued {
		return Reply{Text: "訂閱已建立，系統將在重啟後生效：\n" + access.SubscriptionURL}, true
	}
	return Reply{Text: "你的私人訂閱連結：\n" + access.SubscriptionURL}, true
}

func (handler CommandHandler) isAdminLoginCommand(text string) bool {
	if text == "/adminlogin" {
		return true
	}
	return handler.botUsername != "" && text == "/adminlogin@"+handler.botUsername
}

func (handler CommandHandler) isVPNCommand(text string) bool {
	if text == "/vpn" {
		return true
	}
	return handler.botUsername != "" && text == "/vpn@"+handler.botUsername
}
