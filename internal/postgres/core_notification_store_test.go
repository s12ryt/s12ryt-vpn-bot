package postgres

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestQualificationStoreListsOnlyActiveEligibleVPNRecipients(t *testing.T) {
	database := &databaseStub{row: &rowStub{values: []any{[]int64{101, 202}}}}
	store := NewQualificationStore(database)

	ids, err := store.ActiveVPNUserIDs(context.Background())
	if err != nil {
		t.Fatalf("ActiveVPNUserIDs() error = %v", err)
	}
	if !reflect.DeepEqual(ids, []int64{101, 202}) {
		t.Fatalf("ActiveVPNUserIDs() = %v", ids)
	}
	if !strings.Contains(database.querySQL, "status = 'active'") ||
		!strings.Contains(database.querySQL, "eligible = TRUE") ||
		!strings.Contains(database.querySQL, "ORDER BY telegram_id") {
		t.Fatalf("ActiveVPNUserIDs() query = %q", database.querySQL)
	}
}

func TestAuditStoreRecordsOnlyCoreNotificationCountsAndClosedFailureCode(t *testing.T) {
	now := time.Date(2026, time.August, 17, 10, 0, 0, 0, time.UTC)
	database := &databaseStub{}
	store := NewAuditStore(database)

	if err := store.RecordPlannedRestartNotification(context.Background(), 100, 3, now); err != nil {
		t.Fatalf("RecordPlannedRestartNotification() error = %v", err)
	}
	if !strings.Contains(database.execSQL, "INSERT INTO audit_events") ||
		!strings.Contains(database.execSQL, "core.planned_restart_notification") ||
		!reflect.DeepEqual(database.execArgs, []any{100, 3, now}) {
		t.Fatalf("planned audit query = %q args=%#v", database.execSQL, database.execArgs)
	}

	if err := store.RecordCoreFailureNotification(context.Background(), CoreFailureCheck, 2, 1, now); err != nil {
		t.Fatalf("RecordCoreFailureNotification() error = %v", err)
	}
	if !strings.Contains(database.execSQL, "core.failure_notification") ||
		!reflect.DeepEqual(database.execArgs, []any{string(CoreFailureCheck), 2, 1, now}) {
		t.Fatalf("failure audit query = %q args=%#v", database.execSQL, database.execArgs)
	}
}

func TestAuditStoreRejectsInvalidNotificationMetricsBeforeDatabase(t *testing.T) {
	for _, test := range []struct {
		name string
		call func(*AuditStore) error
	}{
		{name: "failed exceeds attempted", call: func(store *AuditStore) error {
			return store.RecordPlannedRestartNotification(context.Background(), 1, 2, time.Now())
		}},
		{name: "invalid failure", call: func(store *AuditStore) error {
			return store.RecordCoreFailureNotification(context.Background(), CoreFailureCode("secret"), 1, 0, time.Now())
		}},
		{name: "zero time", call: func(store *AuditStore) error {
			return store.RecordCoreFailureNotification(context.Background(), CoreFailureCheck, 1, 0, time.Time{})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := &databaseStub{}
			if err := test.call(NewAuditStore(database)); err == nil {
				t.Fatal("invalid audit record error = nil")
			}
			if database.execSQL != "" {
				t.Fatalf("invalid audit record reached database: %q", database.execSQL)
			}
		})
	}
}

func TestAuditStoreRecordsTrafficNotificationsWithClosedFieldsOnly(t *testing.T) {
	now := time.Date(2026, time.August, 17, 11, 0, 0, 0, time.UTC)
	database := &databaseStub{}
	store := NewAuditStore(database)
	if err := store.RecordTrafficFailureNotification(context.Background(), "record", true, 3, 1, now); err != nil {
		t.Fatalf("RecordTrafficFailureNotification() error = %v", err)
	}
	if !strings.Contains(database.execSQL, "traffic.failure_notification") ||
		!reflect.DeepEqual(database.execArgs, []any{"record", true, 3, 1, now}) {
		t.Fatalf("failure audit query=%q args=%#v", database.execSQL, database.execArgs)
	}
	if err := store.RecordTrafficRecoveryNotification(context.Background(), true, 3, 0, now); err != nil {
		t.Fatalf("RecordTrafficRecoveryNotification() error = %v", err)
	}
	if !strings.Contains(database.execSQL, "traffic.recovery_notification") ||
		!reflect.DeepEqual(database.execArgs, []any{true, 3, 0, now}) {
		t.Fatalf("recovery audit query=%q args=%#v", database.execSQL, database.execArgs)
	}
}

func TestAuditStoreRejectsUnknownTrafficFailureStage(t *testing.T) {
	database := &databaseStub{}
	err := NewAuditStore(database).RecordTrafficFailureNotification(context.Background(), "password", false, 1, 0, time.Now())
	if err == nil || database.execSQL != "" {
		t.Fatalf("error=%v query=%q", err, database.execSQL)
	}
}
