package postgres

import (
	"context"
	"testing"
)

func TestTransactionRunnerRejectsMissingDatabase(t *testing.T) {
	runner := NewTransactionRunner(nil)
	if err := runner.RunInTransaction(context.Background(), func(Database) error { return nil }); err == nil {
		t.Fatal("RunInTransaction() error = nil with no database")
	}
}
