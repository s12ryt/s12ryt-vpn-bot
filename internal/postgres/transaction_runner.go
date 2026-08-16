package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type TransactionBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type transactionRunner struct {
	database TransactionBeginner
}

func NewTransactionRunner(database TransactionBeginner) TransactionRunner {
	return &transactionRunner{database: database}
}

func (runner *transactionRunner) RunInTransaction(ctx context.Context, operation func(Database) error) (resultErr error) {
	if runner == nil || runner.database == nil {
		return errors.New("transaction database is required")
	}
	if operation == nil {
		return errors.New("transaction operation is required")
	}
	transaction, err := runner.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err := transaction.Rollback(context.WithoutCancel(ctx)); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			resultErr = errors.Join(resultErr, fmt.Errorf("rollback transaction: %w", err))
		}
	}()
	if err := operation(transaction); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
