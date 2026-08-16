package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/qualification"
	"github.com/s12ryt/s12ryt-vpn-bot/internal/telegram"
)

func TestQualificationStoreLoadsModeAndEnabledRulesInStableOrder(t *testing.T) {
	database := &databaseStub{row: &rowStub{values: []any{string(domain.QualificationAll), []int64{-1002, -1001}}}}
	store := NewQualificationStore(database)

	mode, rules, err := store.ActiveRules(context.Background())
	if err != nil {
		t.Fatalf("ActiveRules() error = %v", err)
	}
	if mode != domain.QualificationAll {
		t.Fatalf("mode = %q, want all", mode)
	}
	want := []qualification.Rule{{ChatID: -1002}, {ChatID: -1001}}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("rules = %#v, want %#v", rules, want)
	}
	if !strings.Contains(database.querySQL, "qualification_settings") || !strings.Contains(database.querySQL, "rule.enabled") || !strings.Contains(database.querySQL, "ORDER BY rule.chat_id") {
		t.Fatalf("ActiveRules() query = %q", database.querySQL)
	}
}

func TestQualificationStoreRejectsInvalidPersistedModeAndRule(t *testing.T) {
	for _, values := range [][]any{
		{string("invalid"), []int64{-1001}},
		{string(domain.QualificationAny), []int64{0}},
	} {
		store := NewQualificationStore(&databaseStub{row: &rowStub{values: values}})
		if _, _, err := store.ActiveRules(context.Background()); err == nil {
			t.Fatalf("ActiveRules() accepted %#v", values)
		}
	}
}

func TestQualificationStorePropagatesQueryFailure(t *testing.T) {
	wantErr := errors.New("database unavailable")
	store := NewQualificationStore(&databaseStub{row: &rowStub{err: wantErr}})
	if _, _, err := store.ActiveRules(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("ActiveRules() error = %v, want query failure", err)
	}
}

func TestQualificationStoreUpsertsOnlyVerifiedEnabledRule(t *testing.T) {
	database := &databaseStub{}
	store := NewQualificationStore(database)
	verifiedAt := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	rule := qualification.ManagedRule{ChatID: -1001, ChatType: telegram.ChatSupergroup, Title: "資格群"}

	if err := store.UpsertVerified(context.Background(), rule, verifiedAt); err != nil {
		t.Fatalf("UpsertVerified() error = %v", err)
	}
	if !strings.Contains(database.execSQL, "INSERT INTO qualification_rules") ||
		!strings.Contains(database.execSQL, "enabled = TRUE") ||
		!strings.Contains(database.execSQL, "bot_admin_verified_at") {
		t.Fatalf("UpsertVerified() query = %q", database.execSQL)
	}
	if !reflect.DeepEqual(database.execArgs, []any{int64(-1001), string(telegram.ChatSupergroup), "資格群", verifiedAt}) {
		t.Fatalf("UpsertVerified() args = %#v", database.execArgs)
	}
}

func TestQualificationStoreListsKnownUsersAfterStableCursor(t *testing.T) {
	database := &databaseStub{row: &rowStub{values: []any{[]int64{1002, 1003}}}}
	store := NewQualificationStore(database)

	users, err := store.KnownUsersAfter(context.Background(), 1001, 50)
	if err != nil {
		t.Fatalf("KnownUsersAfter() error = %v", err)
	}
	if !reflect.DeepEqual(users, []int64{1002, 1003}) {
		t.Fatalf("KnownUsersAfter() = %v", users)
	}
	if !strings.Contains(database.querySQL, "telegram_id > $1") ||
		!strings.Contains(database.querySQL, "ORDER BY telegram_id") ||
		!strings.Contains(database.querySQL, "LIMIT $2") {
		t.Fatalf("KnownUsersAfter() query = %q", database.querySQL)
	}
	if !reflect.DeepEqual(database.queryArgs, []any{int64(1001), 50}) {
		t.Fatalf("KnownUsersAfter() args = %#v", database.queryArgs)
	}
}

func TestQualificationStoreRejectsInvalidKnownUserPageRequest(t *testing.T) {
	for _, testCase := range []struct {
		after int64
		limit int
	}{
		{after: -1, limit: 50},
		{after: 0, limit: 9},
		{after: 0, limit: 201},
	} {
		database := &databaseStub{}
		store := NewQualificationStore(database)
		if _, err := store.KnownUsersAfter(context.Background(), testCase.after, testCase.limit); err == nil {
			t.Fatalf("KnownUsersAfter(%d, %d) error = nil", testCase.after, testCase.limit)
		}
		if database.querySQL != "" {
			t.Fatalf("invalid request executed query %q", database.querySQL)
		}
	}
}

func TestQualificationStoreLoadsRecheckSettings(t *testing.T) {
	database := &databaseStub{row: &rowStub{values: []any{60, 10, 50}}}
	store := NewQualificationStore(database)

	settings, err := store.RecheckSettings(context.Background())
	if err != nil {
		t.Fatalf("RecheckSettings() error = %v", err)
	}
	want := qualification.RecheckSettings{
		Interval:          time.Hour,
		RequestsPerSecond: 10,
		BatchSize:         50,
	}
	if settings != want {
		t.Fatalf("RecheckSettings() = %#v, want %#v", settings, want)
	}
	if !strings.Contains(database.querySQL, "recheck_interval_minutes") ||
		!strings.Contains(database.querySQL, "recheck_requests_per_second") ||
		!strings.Contains(database.querySQL, "recheck_batch_size") {
		t.Fatalf("RecheckSettings() query = %q", database.querySQL)
	}
}

func TestQualificationStoreRejectsInvalidPersistedRecheckSettings(t *testing.T) {
	for _, values := range [][]any{
		{0, 10, 50},
		{60, 0, 50},
		{60, 21, 50},
		{60, 10, 9},
		{60, 10, 201},
	} {
		store := NewQualificationStore(&databaseStub{row: &rowStub{values: values}})
		if _, err := store.RecheckSettings(context.Background()); err == nil {
			t.Fatalf("RecheckSettings() accepted %#v", values)
		}
	}
}
