package repository

import (
	"errors"

	"github.com/go-sql-driver/mysql"
)

var (
	ErrNotFound  = errors.New("record not found")
	ErrDuplicate = errors.New("duplicate record")
)

const mysqlDuplicateEntry = 1062

func isDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == mysqlDuplicateEntry
}
