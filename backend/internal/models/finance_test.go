package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMoney(t *testing.T) {
	for in, want := range map[string]Money{`12`: 1200, `12.5`: 1250, `12.05`: 1205, `"7.10"`: 710, `0.99`: 99, `-3.25`: -325} {
		var m Money
		if err := json.Unmarshal([]byte(in), &m); err != nil || m != want {
			t.Errorf("%s: got %d %v, want %d", in, m, err, want)
		}
	}
	for _, bad := range []string{`12.345`, `1e3`, `12.`, `.5`, `"abc"`, `1,000`, `true`, `9999999999999`} {
		var m Money
		if err := json.Unmarshal([]byte(bad), &m); err == nil {
			t.Errorf("%s: accepted as %d", bad, m)
		}
	}
	b, _ := json.Marshal(struct{ A, B, C Money }{1250, 5, -1999})
	if string(b) != `{"A":12.50,"B":0.05,"C":-19.99}` {
		t.Errorf("marshal: %s", b)
	}
}

func TestBillNextDue(t *testing.T) {
	d := func(s string) time.Time { v, _ := time.Parse(DateLayout, s); return v }
	day31 := 31
	cases := []struct {
		bill Bill
		from string
		want string
	}{
		{Bill{Frequency: BillMonthly, DueDay: &day31}, "2026-01-31", "2026-02-28"},
		{Bill{Frequency: BillMonthly, DueDay: &day31}, "2026-02-28", "2026-03-31"},
		{Bill{Frequency: BillMonthly}, "2026-10-05", "2026-11-05"},
		{Bill{Frequency: BillWeekly}, "2026-12-29", "2027-01-05"},
		{Bill{Frequency: BillQuarterly, DueDay: &day31}, "2026-11-30", "2027-02-28"},
		{Bill{Frequency: BillYearly}, "2028-02-29", "2029-02-28"},
	}
	for _, c := range cases {
		if got := c.bill.NextDue(d(c.from)).Format(DateLayout); got != c.want {
			t.Errorf("%s %s: got %s, want %s", c.bill.Frequency, c.from, got, c.want)
		}
	}
}
