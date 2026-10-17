package services

import (
	"testing"
	"time"

	"lifeos-ai/backend/internal/models"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestAddMonthsClamped(t *testing.T) {
	cases := []struct {
		start string
		n     int
		want  string
	}{
		{"2026-01-31T09:00:00Z", 1, "2026-02-28T09:00:00Z"},
		{"2028-01-31T09:00:00Z", 1, "2028-02-29T09:00:00Z"}, // leap year
		{"2026-01-31T09:00:00Z", 2, "2026-03-31T09:00:00Z"}, // from anchor: no drift
		{"2026-03-31T09:00:00Z", 1, "2026-04-30T09:00:00Z"},
		{"2026-11-15T09:00:00Z", 3, "2027-02-15T09:00:00Z"}, // year rollover
		{"2028-02-29T09:00:00Z", 12, "2029-02-28T09:00:00Z"},
	}
	for _, c := range cases {
		if got := addMonthsClamped(at(c.start), c.n); !got.Equal(at(c.want)) {
			t.Errorf("addMonthsClamped(%s, %d) = %s, want %s", c.start, c.n, got.Format(time.RFC3339), c.want)
		}
	}
}

func TestNextOccurrence(t *testing.T) {
	until := at("2026-12-31T23:59:59Z")
	cases := []struct {
		name        string
		rec         models.Recurrence
		anchor, due string
		now         string
		want        string // "" = series ended
	}{
		{"daily, completed on time", models.Recurrence{Frequency: "daily", Interval: 1},
			"2026-10-17T09:00:00Z", "2026-10-17T09:00:00Z", "2026-10-17T08:00:00Z", "2026-10-18T09:00:00Z"},
		{"daily, completed late skips missed days", models.Recurrence{Frequency: "daily", Interval: 1},
			"2026-10-10T09:00:00Z", "2026-10-10T09:00:00Z", "2026-10-17T12:00:00Z", "2026-10-18T09:00:00Z"},
		{"every 2 weeks", models.Recurrence{Frequency: "weekly", Interval: 2},
			"2026-10-05T09:00:00Z", "2026-10-05T09:00:00Z", "2026-10-01T00:00:00Z", "2026-10-19T09:00:00Z"},
		{"completed early keeps schedule", models.Recurrence{Frequency: "weekly", Interval: 1},
			"2026-10-20T09:00:00Z", "2026-10-20T09:00:00Z", "2026-10-15T00:00:00Z", "2026-10-27T09:00:00Z"},
		{"monthly on the 31st does not drift", models.Recurrence{Frequency: "monthly", Interval: 1},
			"2026-01-31T09:00:00Z", "2026-02-28T09:00:00Z", "2026-02-20T00:00:00Z", "2026-03-31T09:00:00Z"},
		{"monthly bill paid late", models.Recurrence{Frequency: "monthly", Interval: 1},
			"2026-10-05T09:00:00Z", "2026-10-05T09:00:00Z", "2026-10-10T00:00:00Z", "2026-11-05T09:00:00Z"},
		{"yearly", models.Recurrence{Frequency: "yearly", Interval: 1},
			"2026-03-15T00:00:00Z", "2026-03-15T00:00:00Z", "2026-03-01T00:00:00Z", "2027-03-15T00:00:00Z"},
		{"until reached ends series", models.Recurrence{Frequency: "monthly", Interval: 1, Until: &until},
			"2026-12-15T09:00:00Z", "2026-12-15T09:00:00Z", "2026-12-01T00:00:00Z", ""},
		{"until not yet reached", models.Recurrence{Frequency: "monthly", Interval: 1, Until: &until},
			"2026-11-15T09:00:00Z", "2026-11-15T09:00:00Z", "2026-11-01T00:00:00Z", "2026-12-15T09:00:00Z"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := nextOccurrence(c.rec, at(c.anchor), at(c.due), at(c.now), time.UTC)
			if c.want == "" {
				if ok {
					t.Fatalf("expected series to end, got %s", got.Format(time.RFC3339))
				}
				return
			}
			if !ok || !got.Equal(at(c.want)) {
				t.Fatalf("got %s (ok=%v), want %s", got.Format(time.RFC3339), ok, c.want)
			}
		})
	}
}

func TestDependsTransitively(t *testing.T) {
	// 1 → 2 → 3 (1 depends on 2, 2 depends on 3)
	graph := map[uint64][]uint64{1: {2}, 2: {3}}
	// AddDependency(task=3, dependsOn=1) checks dependsTransitively(graph, 1, 3):
	// 1 already reaches 3 through 2, so the new edge would close a loop.
	if !dependsTransitively(graph, 1, 3) {
		t.Error("1 should depend on 3 through 2 (cycle must be detected)")
	}
	// AddDependency(task=1, dependsOn=3) is fine: 3 does not reach 1.
	if dependsTransitively(graph, 3, 1) {
		t.Error("3 must not depend on 1")
	}
}
