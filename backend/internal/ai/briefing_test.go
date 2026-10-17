package ai

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sampleDay() dayData {
	return dayData{
		openTasks: map[uint64]string{1: "Pay rent", 2: "Call bank"},
		events:    map[uint64]string{7: "Standup"},
	}
}

func TestCleanMorningDropsInventedData(t *testing.T) {
	raw := json.RawMessage(`{
		"summary":"  A busy day. Card 4111 1111 1111 1111 noted.  ",
		"focus":[{"task_id":99,"reason":"invented"},{"task_id":1,"reason":"Due today"},{"task_id":1,"reason":"dup"},{"task_id":2,"reason":"b"}],
		"schedule":[
			{"start":"14:00","end":"15:00","kind":"task","task_id":2,"title":"IGNORED TITLE"},
			{"start":"09:00","end":"09:15","kind":"event","event_id":7},
			{"start":"10:00","end":"10:30","kind":"event","event_id":8},
			{"start":"25:00","end":"26:00","kind":"break"},
			{"start":"12:00","end":"11:00","kind":"break"},
			{"start":"12:30","end":"13:00","kind":"break"},
			{"start":"16:00","end":"17:00","kind":"meeting"}
		],
		"warnings":["Rent is due", "", "Second"]
	}`)
	p, err := sampleDay().cleanMorning(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(p.Summary, "4111") || strings.HasPrefix(p.Summary, " ") {
		t.Errorf("summary: %q", p.Summary)
	}
	if len(p.Focus) != 2 || p.Focus[0].TaskID != 1 || p.Focus[0].Title != "Pay rent" || p.Focus[1].Title != "Call bank" {
		t.Errorf("focus: %+v", p.Focus)
	}
	if len(p.Schedule) != 3 {
		t.Fatalf("schedule: %+v", p.Schedule)
	}
	if p.Schedule[0].Title != "Standup" || p.Schedule[1].Title != "Break" || p.Schedule[2].Title != "Call bank" {
		t.Errorf("titles come from the data and the schedule is sorted: %+v", p.Schedule)
	}
	if len(p.Warnings) != 2 {
		t.Errorf("warnings: %v", p.Warnings)
	}
	if _, err := sampleDay().cleanMorning(json.RawMessage(`{"summary":" "}`)); err == nil {
		t.Error("an empty summary is an error")
	}
}

func TestCleanEvening(t *testing.T) {
	raw := json.RawMessage(`{"summary":"Good day.","wins":["Paid rent",""],"tomorrow":"Standup at 9.",
		"unfinished":[{"task_id":2,"suggestion":"drop","reason":"Carried 5 times"},{"task_id":99,"suggestion":"tomorrow","reason":"x"},
		{"task_id":1,"suggestion":"someday","reason":"bad suggestion"}]}`)
	s, err := sampleDay().cleanEvening(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Unfinished) != 2 || s.Unfinished[0].Title != "Call bank" || s.Unfinished[0].Suggestion != "drop" || s.Unfinished[1].Suggestion != "tomorrow" {
		t.Errorf("unfinished: %+v", s.Unfinished)
	}
	if len(s.Wins) != 1 || s.Tomorrow != "Standup at 9." {
		t.Errorf("got %+v", s)
	}
}

func TestDueAt(t *testing.T) {
	dhaka, _ := time.LoadLocation("Asia/Dhaka")
	now, _ := time.Parse(time.RFC3339, "2026-10-17T02:00:00Z")                                                                          // 08:00 Dhaka
	cases := map[string]bool{"07:30": true, "08:00": true, "08:01": false, "05:30": true, "05:00": false, "04:59": false, "bad": false} // 3 h window
	for at, want := range cases {
		if got := dueAt(now, dhaka, at); got != want {
			t.Errorf("%s: got %v, want %v", at, got, want)
		}
	}
}

func TestEmptyDayBriefing(t *testing.T) {
	for _, kind := range []string{"morning", "evening"} {
		var v map[string]any
		if err := json.Unmarshal(emptyDayBriefing(kind), &v); err != nil || v["summary"] == "" {
			t.Errorf("%s: %v %v", kind, v, err)
		}
	}
	if !(dayData{}).empty() || sampleDay().empty() != true {
		t.Error("a day with nothing listed is empty")
	}
}
