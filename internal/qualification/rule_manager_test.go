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

type ruleWriterStub struct {
	calls      int
	rule       ManagedRule
	verifiedAt time.Time
	err        error
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
