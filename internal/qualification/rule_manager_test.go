package qualification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
)

func TestRuleManagerEnablesRuleOnlyAfterBotAdministratorCheck(t *testing.T) {
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	members := &memberLookupStub{members: map[int64]telegram.ChatMember{
		-1001: {User: telegram.User{ID: 777}, Status: "administrator"},
	}}
	writes := &ruleWriterStub{}
	trigger := &recheckTriggerStub{}
	manager := NewRuleManager(777, members, writes, func() time.Time { return now }, trigger)
	rule := ManagedRule{ChatID: -1001, ChatType: telegram.ChatSupergroup, Title: "資格群"}

	if err := manager.Enable(context.Background(), rule); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if writes.calls != 1 || writes.rule != rule || !writes.verifiedAt.Equal(now) {
		t.Fatalf("rule write = calls %d rule %#v verified %v", writes.calls, writes.rule, writes.verifiedAt)
	}
	if trigger.calls != 1 {
		t.Fatalf("recheck trigger calls = %d, want 1", trigger.calls)
	}
}

func TestRuleManagerRejectsNonAdministratorAndTemporaryFailureWithoutWriting(t *testing.T) {
	for _, lookup := range []*memberLookupStub{
		{members: map[int64]telegram.ChatMember{-1001: {User: telegram.User{ID: 777}, Status: "member"}}},
		{errors: map[int64]error{-1001: errors.New("temporary Telegram failure")}},
	} {
		writes := &ruleWriterStub{}
		manager := NewRuleManager(777, lookup, writes, time.Now)
		err := manager.Enable(context.Background(), ManagedRule{ChatID: -1001, ChatType: telegram.ChatSupergroup})
		if err == nil {
			t.Fatal("Enable() error = nil")
		}
		if writes.calls != 0 {
			t.Fatalf("rule writes = %d, want 0", writes.calls)
		}
	}
}

func TestRuleManagerRejectsInvalidRuleBeforeTelegramRequest(t *testing.T) {
	members := &memberLookupStub{}
	manager := NewRuleManager(777, members, &ruleWriterStub{}, time.Now)

	for _, rule := range []ManagedRule{
		{ChatID: 0, ChatType: telegram.ChatSupergroup},
		{ChatID: -1001, ChatType: telegram.ChatPrivate},
	} {
		if err := manager.Enable(context.Background(), rule); err == nil {
			t.Fatalf("Enable(%#v) error = nil", rule)
		}
	}
	if len(members.calls) != 0 {
		t.Fatalf("Telegram calls = %#v, want none", members.calls)
	}
}

func TestRuleManagerEnablesAndDisablesAuditedRules(t *testing.T) {
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	members := &memberLookupStub{members: map[int64]telegram.ChatMember{
		-1001: {User: telegram.User{ID: 777}, Status: "creator"},
	}}
	writes := &ruleWriterStub{}
	trigger := &recheckTriggerStub{}
	manager := NewRuleManager(777, members, writes, func() time.Time { return now }, trigger)
	rule := ManagedRule{ChatID: -1001, ChatType: telegram.ChatChannel, Title: "公告"}

	if err := manager.EnableByActor(context.Background(), 9001, rule); err != nil {
		t.Fatalf("EnableByActor() error = %v", err)
	}
	if writes.actor != 9001 || writes.calls != 1 {
		t.Fatalf("enable actor=%d calls=%d", writes.actor, writes.calls)
	}
	if err := manager.DisableByActor(context.Background(), 9001, -1001); err != nil {
		t.Fatalf("DisableByActor() error = %v", err)
	}
	if writes.disabledActor != 9001 || writes.disabledChatID != -1001 || trigger.calls != 2 {
		t.Fatalf("disable actor=%d chat=%d triggers=%d", writes.disabledActor, writes.disabledChatID, trigger.calls)
	}
}

func TestRuleManagerRejectsInvalidActorBeforeDependencies(t *testing.T) {
	members := &memberLookupStub{}
	writes := &ruleWriterStub{}
	manager := NewRuleManager(777, members, writes, time.Now)
	if err := manager.EnableByActor(context.Background(), 0, ManagedRule{ChatID: -1001, ChatType: telegram.ChatSupergroup}); err == nil {
		t.Fatal("EnableByActor() accepted zero actor")
	}
	if err := manager.DisableByActor(context.Background(), 0, -1001); err == nil {
		t.Fatal("DisableByActor() accepted zero actor")
	}
	if len(members.calls) != 0 || writes.calls != 0 {
		t.Fatalf("dependencies called: members=%v writes=%d", members.calls, writes.calls)
	}
}

type ruleWriterStub struct {
	calls          int
	rule           ManagedRule
	verifiedAt     time.Time
	err            error
	actor          int64
	disabledActor  int64
	disabledChatID int64
}

func (stub *ruleWriterStub) UpsertVerifiedByActor(ctx context.Context, actor int64, rule ManagedRule, verifiedAt time.Time) error {
	stub.actor = actor
	return stub.UpsertVerified(ctx, rule, verifiedAt)
}

func (stub *ruleWriterStub) DisableByActor(_ context.Context, actor, chatID int64, _ time.Time) error {
	stub.disabledActor, stub.disabledChatID = actor, chatID
	return stub.err
}

type recheckTriggerStub struct {
	calls int
}

func (stub *recheckTriggerStub) Trigger() {
	stub.calls++
}

func (stub *ruleWriterStub) UpsertVerified(_ context.Context, rule ManagedRule, verifiedAt time.Time) error {
	stub.calls++
	stub.rule = rule
	stub.verifiedAt = verifiedAt
	return stub.err
}
