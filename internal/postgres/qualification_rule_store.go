package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/qualification"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
)

type QualificationRuleStore struct{ transactions TransactionRunner }

func NewQualificationRuleStore(transactions TransactionRunner) *QualificationRuleStore {
	return &QualificationRuleStore{transactions: transactions}
}

func (store *QualificationRuleStore) UpsertVerified(context.Context, qualification.ManagedRule, time.Time) error {
	return errors.New("audited qualification rule write is required")
}

func (store *QualificationRuleStore) UpsertVerifiedByActor(ctx context.Context, actor int64, rule qualification.ManagedRule, verifiedAt time.Time) error {
	if store == nil || store.transactions == nil || actor <= 0 || rule.ChatID == 0 || verifiedAt.IsZero() ||
		(rule.ChatType != telegram.ChatSupergroup && rule.ChatType != telegram.ChatChannel) {
		return errors.New("verified qualification rule is invalid")
	}
	return store.transactions.RunInTransaction(ctx, func(transaction Database) error {
		if _, err := transaction.Exec(ctx, `
			INSERT INTO qualification_rules (chat_id, chat_type, title, enabled, bot_admin_verified_at)
			VALUES ($1, $2, $3, TRUE, $4)
			ON CONFLICT (chat_id) DO UPDATE SET
				chat_type = EXCLUDED.chat_type, title = EXCLUDED.title, enabled = TRUE,
				bot_admin_verified_at = EXCLUDED.bot_admin_verified_at, updated_at = $4`,
			rule.ChatID, string(rule.ChatType), strings.TrimSpace(rule.Title), verifiedAt); err != nil {
			return err
		}
		_, err := transaction.Exec(ctx, `
			INSERT INTO audit_events (actor_telegram_id, action, target_type, target_id, details, created_at)
			VALUES ($1, 'qualification_rule.enable', 'qualification_rule', $2, jsonb_build_object('chat_type', $3), $4)`,
			actor, rule.ChatID, string(rule.ChatType), verifiedAt)
		return err
	})
}

func (store *QualificationRuleStore) DisableByActor(ctx context.Context, actor, chatID int64, disabledAt time.Time) error {
	if store == nil || store.transactions == nil || actor <= 0 || chatID == 0 || disabledAt.IsZero() {
		return errors.New("qualification rule disable request is invalid")
	}
	return store.transactions.RunInTransaction(ctx, func(transaction Database) error {
		if _, err := transaction.Exec(ctx, `
			UPDATE qualification_rules SET enabled = FALSE, updated_at = $2
			WHERE chat_id = $1`, chatID, disabledAt); err != nil {
			return err
		}
		_, err := transaction.Exec(ctx, `
			INSERT INTO audit_events (actor_telegram_id, action, target_type, target_id, details, created_at)
			VALUES ($1, 'qualification_rule.disable', 'qualification_rule', $2, '{}'::jsonb, $3)`, actor, chatID, disabledAt)
		return err
	})
}
