package main

import (
	"context"
	"errors"
	"testing"

	"github.com/s12ryt/s12ryt-vpn-bot/internal/domain"
)

func TestDynamicRetentionUsesLatestDatabaseValueEveryAttempt(t *testing.T) {
	provider := &retentionProviderStub{settings: []domain.BackupSettings{{RetentionDays: 7}, {RetentionDays: 30}}}
	first, ok := retentionForAttempt(context.Background(), provider)
	if !ok || first != 7 {
		t.Fatalf("first retention=%d ok=%v", first, ok)
	}
	second, ok := retentionForAttempt(context.Background(), provider)
	if !ok || second != 30 {
		t.Fatalf("second retention=%d ok=%v", second, ok)
	}
}

func TestDynamicRetentionFailureSkipsPruning(t *testing.T) {
	provider := &retentionProviderStub{err: errors.New("database unavailable")}
	if retention, ok := retentionForAttempt(context.Background(), provider); ok || retention != 0 {
		t.Fatalf("retention=%d ok=%v", retention, ok)
	}
}

type retentionProviderStub struct {
	settings []domain.BackupSettings
	err      error
}

func (stub *retentionProviderStub) Get(context.Context) (domain.BackupSettings, error) {
	if stub.err != nil {
		return domain.BackupSettings{}, stub.err
	}
	settings := stub.settings[0]
	stub.settings = stub.settings[1:]
	return settings, nil
}
