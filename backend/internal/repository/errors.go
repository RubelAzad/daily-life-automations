package repository

import (
	"errors"

	"github.com/go-sql-driver/mysql"
)

var (
	ErrNotFound  = errors.New("record not found")
	ErrDuplicate = errors.New("duplicate record")
)

const (
	mysqlDuplicateEntry  = 1062
	mysqlLockWaitTimeout = 1205
	mysqlDeadlock        = 1213
)

func isDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == mysqlDuplicateEntry
}

// isRetryable reports a deadlock or lock wait timeout: the transaction was
// rolled back and can simply be run again.
func isRetryable(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && (me.Number == mysqlDeadlock || me.Number == mysqlLockWaitTimeout)
}

// retry runs fn up to three times while it fails with a deadlock.
func retry(fn func() error) error {
	var err error
	for i := 0; i < 3; i++ {
		if err = fn(); !isRetryable(err) {
			return err
		}
	}
	return err
}
