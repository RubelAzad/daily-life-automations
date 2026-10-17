package models

import (
	"database/sql/driver"
	"fmt"
	"time"
)

const DateLayout = "2006-01-02"

// Date is a calendar date with no time or zone (a MySQL DATE), serialized as
// "YYYY-MM-DD". Use it for things like a project's target date, where "the
// 20th" must not shift with the viewer's time zone.
type Date struct{ time.Time }

func (d Date) String() string { return d.Format(DateLayout) }

func (d Date) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.String() + `"`), nil
}

func (d *Date) Scan(src any) error {
	t, ok := src.(time.Time)
	if !ok {
		return fmt.Errorf("models.Date: cannot scan %T", src)
	}
	d.Time = t
	return nil
}

func (d Date) Value() (driver.Value, error) {
	return d.String(), nil
}
