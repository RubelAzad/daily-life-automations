package automation

import (
	"testing"
	"time"

	"lifeos-ai/backend/internal/models"
)

func TestHappensBefore(t *testing.T) {
	ts := func(s string) *time.Time {
		v, _ := time.Parse(time.RFC3339, s)
		return &v
	}
	quietEnd := *ts("2026-10-18T01:00:00Z")
	cases := []struct {
		name     string
		r        models.Reminder
		critical bool
	}{
		{"event before quiet hours end", models.Reminder{RemindAt: ts("2026-10-17T23:00:00Z"), OccursAt: ts("2026-10-18T00:00:00Z")}, true},
		{"event after quiet hours end", models.Reminder{RemindAt: ts("2026-10-17T23:00:00Z"), OccursAt: ts("2026-10-18T03:00:00Z")}, false},
		{"at due time", models.Reminder{RemindAt: ts("2026-10-17T23:00:00Z"), OccursAt: ts("2026-10-17T23:00:00Z")}, true},
		{"follow-up after due", models.Reminder{RemindAt: ts("2026-10-17T23:30:00Z"), OccursAt: ts("2026-10-17T23:00:00Z")}, false},
		{"standalone", models.Reminder{RemindAt: ts("2026-10-17T23:00:00Z")}, false},
	}
	for _, c := range cases {
		if got := happensBefore(c.r, quietEnd); got != c.critical {
			t.Errorf("%s: got %v, want %v", c.name, got, c.critical)
		}
	}
}
