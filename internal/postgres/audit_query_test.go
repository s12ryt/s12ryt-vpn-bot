package postgres

import (
	"context"
	"strings"
	"testing"
)

func TestAuditStoreListsNewestEventsWithStableCursor(t *testing.T) {
	database := &accessTransactionStub{row: &accessRowStub{values: []any{[]byte(`[{"id":9,"actor_telegram_id":77,"action":"vpn.revoke","target_type":"vpn_user","target_id":"123","details":{"mode":"requires_approval"},"created_at":"2026-08-17T00:00:00Z"}]`)}}}
	store := NewAuditStore(database)
	events, err := store.List(context.Background(), 10, 50)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(events) != 1 || events[0].ID != 9 || events[0].ActorTelegramID == nil || *events[0].ActorTelegramID != 77 || string(events[0].Details) != `{"mode":"requires_approval"}` {
		t.Fatalf("events = %#v", events)
	}
	if !strings.Contains(database.querySQL, "id < $1") || !strings.Contains(database.querySQL, "ORDER BY audit_event.id DESC") || strings.Contains(strings.ToLower(database.querySQL), "credential_bundles") {
		t.Fatalf("query = %q", database.querySQL)
	}
}

func TestAuditStoreRejectsInvalidWindowAndMalformedEvent(t *testing.T) {
	database := &accessTransactionStub{}
	store := NewAuditStore(database)
	if _, err := store.List(context.Background(), -1, 50); err == nil || database.querySQL != "" {
		t.Fatalf("invalid window error=%v query=%q", err, database.querySQL)
	}
	database.row = &accessRowStub{values: []any{[]byte(`[{"id":1,"action":"x","target_type":"y","target_id":"","details":[],"created_at":"2026-08-17T00:00:00Z"}]`)}}
	if events, err := store.List(context.Background(), 0, 50); err == nil || events != nil {
		t.Fatalf("malformed events=%#v error=%v", events, err)
	}
}
