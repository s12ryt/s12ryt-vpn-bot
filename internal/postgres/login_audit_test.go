package postgres

import (
	"context"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAuditStoreRecordsClosedLoginOutcomeWithoutCode(t *testing.T) {
	now := time.Date(2026, time.August, 17, 14, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name       string
		success    bool
		wantAction string
	}{
		{name: "success", success: true, wantAction: "auth.login.success"},
		{name: "failure", success: false, wantAction: "auth.login.failure"},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := &databaseStub{}
			store := NewAuditStore(database)

			if err := store.RecordLoginAttempt(context.Background(), 12345, netip.MustParseAddr("198.51.100.8"), test.success, now); err != nil {
				t.Fatalf("RecordLoginAttempt() error = %v", err)
			}
			if !strings.Contains(database.execSQL, "INSERT INTO audit_events") || strings.Contains(strings.ToLower(database.execSQL), "code") {
				t.Fatalf("login audit SQL = %q", database.execSQL)
			}
			wantArgs := []any{test.wantAction, "12345", "198.51.100.8", now}
			if !reflect.DeepEqual(database.execArgs, wantArgs) {
				t.Fatalf("login audit args = %#v, want %#v", database.execArgs, wantArgs)
			}
			for _, argument := range database.execArgs {
				if argument == "Ab12Cd34" {
					t.Fatal("login audit persisted the raw login code")
				}
			}
		})
	}
}

func TestAuditStoreRejectsInvalidLoginAuditBeforeDatabase(t *testing.T) {
	for _, test := range []struct {
		name       string
		telegramID int64
		sourceIP   netip.Addr
		at         time.Time
	}{
		{name: "telegram ID", sourceIP: netip.MustParseAddr("198.51.100.8"), at: time.Now()},
		{name: "source IP", telegramID: 12345, at: time.Now()},
		{name: "time", telegramID: 12345, sourceIP: netip.MustParseAddr("198.51.100.8")},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := &databaseStub{}
			err := NewAuditStore(database).RecordLoginAttempt(context.Background(), test.telegramID, test.sourceIP, false, test.at)
			if err == nil || database.execSQL != "" {
				t.Fatalf("error=%v query=%q, want pre-database rejection", err, database.execSQL)
			}
		})
	}
}
