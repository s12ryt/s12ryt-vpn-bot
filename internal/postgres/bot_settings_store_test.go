package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/secrets"
)

func TestBotSettingsStoreSavesEncryptedTokenAndAudits(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	transaction := &accessTransactionStub{}
	runner := &transactionRunnerStub{transaction: transaction}
	cipher := &coreSettingsCipherStub{sealed: secrets.SealedValue{Nonce: []byte("123456789012"), Ciphertext: []byte("encrypted bot token")}}
	store := NewBotSettingsStore(runner, nil, cipher)

	if err := store.Save(context.Background(), 9001, "new-bot-token", "member_bot", now); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if !runner.committed || runner.rolledBack {
		t.Fatalf("committed=%v rolledBack=%v", runner.committed, runner.rolledBack)
	}
	if cipher.sealPurpose != botTokenPurpose || cipher.sealValue != "new-bot-token" {
		t.Fatalf("Seal(%q, %q)", cipher.sealPurpose, cipher.sealValue)
	}
	if len(transaction.execSQL) != 2 || !strings.Contains(transaction.execSQL[0], "UPDATE bot_settings") ||
		!strings.Contains(transaction.execSQL[1], "bot.token.update") {
		t.Fatalf("statements = %#v", transaction.execSQL)
	}
	for _, arguments := range transaction.execArgs {
		for _, argument := range arguments {
			if text, ok := argument.(string); ok && text == "new-bot-token" {
				t.Fatal("database arguments contain plaintext bot token")
			}
		}
	}
}

func TestBotSettingsStoreRejectsInvalidInputBeforeTransaction(t *testing.T) {
	runner := &transactionRunnerStub{}
	store := NewBotSettingsStore(runner, nil, &coreSettingsCipherStub{})
	tests := []struct {
		actor    int64
		token    string
		username string
	}{
		{actor: 0, token: "token", username: "member_bot"},
		{actor: 9001, token: "", username: "member_bot"},
		{actor: 9001, token: "token", username: ""},
	}
	for _, test := range tests {
		if err := store.Save(context.Background(), test.actor, test.token, test.username, time.Now().UTC()); err == nil || runner.called {
			t.Fatalf("Save(%+v) error=%v transaction=%v", test, err, runner.called)
		}
	}
}

func TestBotSettingsStoreTokenDecryptsStoredValue(t *testing.T) {
	database := &coreSettingsDatabaseStub{row: &botTokenRowStub{values: []any{
		[]byte("123456789012"), []byte("encrypted bot token"),
	}}}
	cipher := &coreSettingsCipherStub{opened: "stored-bot-token"}
	store := NewBotSettingsStore(nil, database, cipher)

	token, err := store.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if token != "stored-bot-token" || cipher.openPurpose != botTokenPurpose {
		t.Fatalf("token=%q openPurpose=%q", token, cipher.openPurpose)
	}
}

func TestBotSettingsStoreTokenRequiresStoredSecret(t *testing.T) {
	database := &coreSettingsDatabaseStub{row: &botTokenRowStub{values: []any{nil, nil}}}
	store := NewBotSettingsStore(nil, database, &coreSettingsCipherStub{})
	if _, err := store.Token(context.Background()); err == nil {
		t.Fatal("missing stored token must be reported")
	}
}

func TestBotSettingsStoreOverviewNeverReadsCiphertext(t *testing.T) {
	database := &coreSettingsDatabaseStub{row: &botOverviewRowStub{values: []any{
		"member_bot", time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC),
	}}}
	store := NewBotSettingsStore(nil, database, nil)

	overview, err := store.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if overview.BotUsername != "member_bot" || !overview.UpdatedAt.Equal(time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("overview = %#v", overview)
	}
	if strings.Contains(database.query, "ciphertext") || strings.Contains(database.query, "nonce") {
		t.Fatalf("overview query reads token material: %q", database.query)
	}
}

type botTokenRowStub struct{ values []any }

func (row *botTokenRowStub) Scan(destinations ...any) error {
	if len(destinations) != len(row.values) {
		return context.Canceled
	}
	for index, destination := range destinations {
		if pointer, ok := destination.(*[]byte); ok {
			if row.values[index] == nil {
				continue
			}
			*pointer = row.values[index].([]byte)
		}
	}
	return nil
}

type botOverviewRowStub struct{ values []any }

func (row *botOverviewRowStub) Scan(destinations ...any) error {
	if len(destinations) != len(row.values) {
		return context.Canceled
	}
	for index, destination := range destinations {
		switch pointer := destination.(type) {
		case *string:
			*pointer = row.values[index].(string)
		case *sql.NullTime:
			*pointer = sql.NullTime{Valid: true, Time: row.values[index].(time.Time)}
		}
	}
	return nil
}
