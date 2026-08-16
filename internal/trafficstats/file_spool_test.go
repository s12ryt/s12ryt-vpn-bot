package trafficstats

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFileSpoolSavesLoadsAndDeletesOnePendingBatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic", "pending.json")
	spool, err := NewFileSpool(path)
	if err != nil {
		t.Fatalf("NewFileSpool() error = %v", err)
	}
	collectedAt := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	want, err := NewPendingBatch(collectedAt, []Sample{
		{TelegramID: 1001, Uplink: 11, Downlink: 13},
		{TelegramID: 2002, Uplink: 17, Downlink: 19},
	})
	if err != nil {
		t.Fatalf("NewPendingBatch() error = %v", err)
	}
	if len(want.ID) != 64 {
		t.Fatalf("batch ID length = %d, want 64 hex characters", len(want.ID))
	}

	if err := spool.Save(context.Background(), want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); runtime.GOOS != "windows" && got != 0o600 {
		t.Fatalf("spool permissions = %#o, want 0600", got)
	}

	got, ok, err := spool.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = (%#v, %t), want (%#v, true)", got, ok, want)
	}
	if err := spool.Save(context.Background(), want); !errors.Is(err, ErrPendingBatchExists) {
		t.Fatalf("second Save() error = %v, want ErrPendingBatchExists", err)
	}

	if err := spool.Delete(context.Background()); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if got, ok, err := spool.Load(context.Background()); err != nil || ok || !reflect.DeepEqual(got, PendingBatch{}) {
		t.Fatalf("Load(after delete) = (%#v, %t, %v), want zero, false, nil", got, ok, err)
	}
	if err := spool.Delete(context.Background()); err != nil {
		t.Fatalf("second Delete() error = %v, want idempotent success", err)
	}
}

func TestFileSpoolRejectsInvalidBatchesBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pending.json")
	spool, err := NewFileSpool(path)
	if err != nil {
		t.Fatalf("NewFileSpool() error = %v", err)
	}
	validTime := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	valid, err := NewPendingBatch(validTime, []Sample{{TelegramID: 1, Uplink: 1}})
	if err != nil {
		t.Fatalf("NewPendingBatch() error = %v", err)
	}
	tests := []struct {
		name  string
		batch PendingBatch
	}{
		{name: "missing id", batch: PendingBatch{CollectedAt: validTime, Samples: valid.Samples}},
		{name: "zero time", batch: PendingBatch{ID: valid.ID, Samples: valid.Samples}},
		{name: "tampered sample", batch: PendingBatch{ID: valid.ID, CollectedAt: validTime, Samples: []Sample{{TelegramID: 1, Uplink: 2}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := spool.Save(context.Background(), test.batch); err == nil {
				t.Fatal("Save() error = nil, want validation error")
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("spool file exists after invalid Save(): %v", err)
			}
		})
	}
}

func TestFileSpoolLoadRejectsCorruptionWithoutPartialBatch(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "pending.json")
	tests := []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"version":1,"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","collected_at":"2026-08-17T12:00:00Z","samples":[],"secret":"leak"}`},
		{name: "wrong version", body: `{"version":2,"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","collected_at":"2026-08-17T12:00:00Z","samples":[]}`},
		{name: "trailing json", body: `{"version":1,"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","collected_at":"2026-08-17T12:00:00Z","samples":[]} {}`},
		{name: "invalid sample", body: `{"version":1,"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","collected_at":"2026-08-17T12:00:00Z","samples":[{"telegram_id":1,"uplink":-1,"downlink":0}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(test.body), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			spool, err := NewFileSpool(path)
			if err != nil {
				t.Fatalf("NewFileSpool() error = %v", err)
			}
			got, ok, err := spool.Load(context.Background())
			if err == nil || ok || !reflect.DeepEqual(got, PendingBatch{}) {
				t.Fatalf("Load() = (%#v, %t, %v), want zero, false, error", got, ok, err)
			}
			if strings.Contains(err.Error(), test.body) {
				t.Fatal("Load() error leaked spool body")
			}
		})
	}
}

func TestNewFileSpoolRejectsUnsafePath(t *testing.T) {
	for _, path := range []string{"", "relative/pending.json", filepath.Clean(string(os.PathSeparator))} {
		if _, err := NewFileSpool(path); err == nil {
			t.Fatalf("NewFileSpool(%q) error = nil, want error", path)
		}
	}
}
