package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/qualification"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
)

func TestQualificationRuleStorePersistsAuditedEnableAndDisable(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	transaction := &accessTransactionStub{}
	runner := &transactionRunnerStub{transaction: transaction}
	store := NewQualificationRuleStore(runner)
	rule := qualification.ManagedRule{ChatID: -1001, ChatType: telegram.ChatSupergroup, Title: "會員群"}

	if err := store.UpsertVerifiedByActor(context.Background(), 9001, rule, now); err != nil {
		t.Fatalf("UpsertVerifiedByActor() error = %v", err)
	}
	if !runner.committed || len(transaction.execSQL) != 2 || !strings.Contains(transaction.execSQL[0], "qualification_rules") || !strings.Contains(transaction.execSQL[1], "audit_events") {
		t.Fatalf("enable committed=%v SQL=%#v", runner.committed, transaction.execSQL)
	}

	transaction = &accessTransactionStub{}
	runner = &transactionRunnerStub{transaction: transaction}
	store = NewQualificationRuleStore(runner)
	if err := store.DisableByActor(context.Background(), 9001, -1001, now); err != nil {
		t.Fatalf("DisableByActor() error = %v", err)
	}
	if !runner.committed || len(transaction.execSQL) != 2 || !strings.Contains(transaction.execSQL[0], "enabled = FALSE") || !strings.Contains(transaction.execSQL[1], "audit_events") {
		t.Fatalf("disable committed=%v SQL=%#v", runner.committed, transaction.execSQL)
	}
}

func TestQualificationRuleStoreRejectsInvalidInputBeforeTransaction(t *testing.T) {
	runner := &transactionRunnerStub{}
	store := NewQualificationRuleStore(runner)
	if err := store.DisableByActor(context.Background(), 0, -1001, time.Now()); err == nil || runner.called {
		t.Fatalf("invalid disable error=%v transaction=%v", err, runner.called)
	}
}
