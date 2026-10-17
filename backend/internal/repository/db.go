package repository

import (
	"context"
	"database/sql"
)

// DBTX is satisfied by both *sql.DB and *sql.Tx, so a repository can run
// inside or outside a transaction.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// inTx runs fn in a transaction, or directly if db is already a transaction.
func inTx(ctx context.Context, db DBTX, fn func(DBTX) error) error {
	sqlDB, ok := db.(*sql.DB)
	if !ok {
		return fn(db)
	}
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
