package telegram

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/auth"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

type Callback struct {
	ID       string
	SenderID int64
	Data     string
}

type ApprovalProvisioner interface {
	Approve(context.Context, int64, time.Time) (domain.ProvisionedAccess, error)
}

type ApprovalDecisionManager interface {
	RejectApproval(context.Context, int64, int64, time.Time) error
}

type ApprovalMessageSender interface {
	SendMessage(context.Context, int64, string) error
}

type CallbackAcknowledger interface {
	AnswerCallbackQuery(context.Context, string, string) error
}

type ApprovalHandler struct {
	administrators auth.AdministratorLookup
	provisioner    ApprovalProvisioner
	decisions      ApprovalDecisionManager
	sender         ApprovalMessageSender
	now            func() time.Time
}

func NewApprovalHandler(administrators auth.AdministratorLookup, provisioner ApprovalProvisioner, decisions ApprovalDecisionManager, sender ApprovalMessageSender, now func() time.Time) (*ApprovalHandler, error) {
	if administrators == nil || provisioner == nil || decisions == nil || sender == nil {
		return nil, errors.New("approval dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &ApprovalHandler{administrators: administrators, provisioner: provisioner, decisions: decisions, sender: sender, now: now}, nil
}

func (handler *ApprovalHandler) HandleCallback(ctx context.Context, callback Callback) error {
	if handler == nil || strings.TrimSpace(callback.ID) == "" || callback.SenderID <= 0 {
		return errors.New("callback is invalid")
	}
	administrator, err := handler.administrators.FindActive(ctx, callback.SenderID)
	if err != nil || administrator.TelegramID != callback.SenderID || !administrator.Active || !administrator.Role.Allows(auth.PermissionManageApprovals) {
		return auth.ErrAdministratorUnauthorized
	}
	action, targetID, err := parseApprovalCallback(callback.Data)
	if err != nil {
		return err
	}
	switch action {
	case "approve":
		if _, err := handler.provisioner.Approve(ctx, targetID, handler.now()); err != nil {
			return err
		}
		_ = handler.sender.SendMessage(context.WithoutCancel(ctx), targetID, "你的 VPN 申請已核准，請使用 /vpn 取得私人訂閱連結。")
		if acknowledger, ok := handler.sender.(CallbackAcknowledger); ok {
			_ = acknowledger.AnswerCallbackQuery(context.WithoutCancel(ctx), callback.ID, "已核准")
		}
	case "reject":
		if err := handler.decisions.RejectApproval(ctx, callback.SenderID, targetID, handler.now()); err != nil {
			return err
		}
		_ = handler.sender.SendMessage(context.WithoutCancel(ctx), targetID, "你的 VPN 申請未獲核准。")
		if acknowledger, ok := handler.sender.(CallbackAcknowledger); ok {
			_ = acknowledger.AnswerCallbackQuery(context.WithoutCancel(ctx), callback.ID, "已拒絕")
		}
	}
	return nil
}

func parseApprovalCallback(data string) (string, int64, error) {
	action, value, ok := strings.Cut(data, ":")
	if !ok || (action != "approve" && action != "reject") || value == "" || strings.Contains(value, ":") {
		return "", 0, errors.New("approval callback is invalid")
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
		return "", 0, errors.New("approval callback is invalid")
	}
	return action, id, nil
}
