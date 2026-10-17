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

// InTx runs fn in a transaction, or directly if db is already a transaction,
// so several repositories can share one: build each with DB: tx.
func InTx(ctx context.Context, db DBTX, fn func(tx DBTX) error) error {
	return inTx(ctx, db, fn)
}

// inTxReadCommitted is InTx at READ COMMITTED: no gap locks, so a
// SELECT ... FOR UPDATE SKIP LOCKED on a queue table doesn't block inserts
// into it. db must be a *sql.DB.
func inTxReadCommitted(ctx context.Context, db DBTX, fn func(DBTX) error) error {
	return beginTx(ctx, db, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, fn)
}

func inTx(ctx context.Context, db DBTX, fn func(DBTX) error) error {
	return beginTx(ctx, db, nil, fn)
}

func beginTx(ctx context.Context, db DBTX, opts *sql.TxOptions, fn func(DBTX) error) error {
	sqlDB, ok := db.(*sql.DB)
	if !ok {
		return fn(db)
	}
	tx, err := sqlDB.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
