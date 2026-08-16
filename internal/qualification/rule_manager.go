package qualification

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
)

var ErrBotAdministratorRequired = errors.New("Bot administrator permission is required")

type ManagedRule struct {
	ChatID   int64
	ChatType telegram.ChatType
	Title    string
}

type VerifiedRuleWriter interface {
	UpsertVerified(ctx context.Context, rule ManagedRule, verifiedAt time.Time) error
}

type AuditedRuleWriter interface {
	UpsertVerifiedByActor(ctx context.Context, actorTelegramID int64, rule ManagedRule, verifiedAt time.Time) error
	DisableByActor(ctx context.Context, actorTelegramID, chatID int64, disabledAt time.Time) error
}

type RecheckTrigger interface {
	Trigger()
}

type RuleManager struct {
	botID   int64
	members MemberLookup
	writer  VerifiedRuleWriter
	now     func() time.Time
	trigger RecheckTrigger
}

func NewRuleManager(botID int64, members MemberLookup, writer VerifiedRuleWriter, now func() time.Time, triggers ...RecheckTrigger) *RuleManager {
	if now == nil {
		now = time.Now
	}
	manager := &RuleManager{botID: botID, members: members, writer: writer, now: now}
	if len(triggers) > 0 {
		manager.trigger = triggers[0]
	}
	return manager
}

func (manager *RuleManager) Enable(ctx context.Context, rule ManagedRule) error {
	return manager.enable(ctx, 0, rule)
}

func (manager *RuleManager) EnableByActor(ctx context.Context, actorTelegramID int64, rule ManagedRule) error {
	if actorTelegramID <= 0 {
		return errors.New("qualification rule actor is invalid")
	}
	return manager.enable(ctx, actorTelegramID, rule)
}

func (manager *RuleManager) enable(ctx context.Context, actorTelegramID int64, rule ManagedRule) error {
	if manager == nil || manager.botID <= 0 || manager.members == nil || manager.writer == nil {
		return errors.New("qualification rule manager dependencies are invalid")
	}
	if rule.ChatID == 0 || (rule.ChatType != telegram.ChatSupergroup && rule.ChatType != telegram.ChatChannel) {
		return errors.New("qualification rule is invalid")
	}
	rule.Title = strings.TrimSpace(rule.Title)
	member, err := manager.members.GetChatMember(ctx, rule.ChatID, manager.botID)
	if err != nil {
		return fmt.Errorf("verify Bot administrator permission: %w", err)
	}
	if member.Status != "administrator" && member.Status != "creator" {
		return ErrBotAdministratorRequired
	}
	verifiedAt := manager.now()
	if verifiedAt.IsZero() {
		return errors.New("verification timestamp is required")
	}
	var writeErr error
	if actorTelegramID > 0 {
		writer, ok := manager.writer.(AuditedRuleWriter)
		if !ok {
			return errors.New("audited qualification rule writer is required")
		}
		writeErr = writer.UpsertVerifiedByActor(ctx, actorTelegramID, rule, verifiedAt)
	} else {
		writeErr = manager.writer.UpsertVerified(ctx, rule, verifiedAt)
	}
	if writeErr != nil {
		return fmt.Errorf("save verified qualification rule: %w", writeErr)
	}
	if manager.trigger != nil {
		manager.trigger.Trigger()
	}
	return nil
}

func (manager *RuleManager) DisableByActor(ctx context.Context, actorTelegramID, chatID int64) error {
	if manager == nil || manager.writer == nil || actorTelegramID <= 0 || chatID == 0 {
		return errors.New("qualification rule disable request is invalid")
	}
	writer, ok := manager.writer.(AuditedRuleWriter)
	if !ok {
		return errors.New("audited qualification rule writer is required")
	}
	disabledAt := manager.now()
	if disabledAt.IsZero() {
		return errors.New("disable timestamp is required")
	}
	if err := writer.DisableByActor(ctx, actorTelegramID, chatID, disabledAt); err != nil {
		return fmt.Errorf("disable qualification rule: %w", err)
	}
	if manager.trigger != nil {
		manager.trigger.Trigger()
	}
	return nil
}
