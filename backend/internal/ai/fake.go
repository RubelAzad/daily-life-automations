package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// FakeClient is an offline stand-in for local development and tests
// (AI_PROVIDER=fake, allowed only with APP_ENV=local). It makes no network
// calls and costs nothing. It follows simple scripts in the user's message:
//
//	fake:tool NAME {json} [&& NAME {json} ...]  call these tools, then summarise
//	fake:loop                                    call get_today forever
//	fake:error                                   fail at once
//	fake:error-after-tool                        call get_today, then fail
//
// Anything else gets a canned text reply.
type FakeClient struct{}

func (FakeClient) Model() string    { return "fake" }
func (FakeClient) Provider() string { return "fake" }

func (FakeClient) Complete(_ context.Context, req Request) (Response, error) {
	if req.ToolChoice != nil {
		return fakeStructured(req)
	}
	script, rounds := lastTypedText(req.Messages)
	reply := func(text string) (Response, error) {
		return Response{Content: []Block{{Type: "text", Text: text}}, StopReason: "end_turn", Model: "fake",
			Usage: Usage{InputTokens: len(script) / 4, OutputTokens: len(text) / 4}}, nil
	}
	call := func(uses ...Block) (Response, error) {
		for i := range uses {
			uses[i].Type, uses[i].ID = "tool_use", fmt.Sprintf("toolu_fake_%d_%d", rounds, i)
		}
		return Response{Content: append([]Block{{Type: "text", Text: "Working on it."}}, uses...), StopReason: "tool_use", Model: "fake",
			Usage: Usage{InputTokens: 10, OutputTokens: 10}}, nil
	}
	cmd := strings.TrimSpace(script)

	switch {
	case strings.Contains(strings.ToLower(cmd), "reply with the single word ok"):
		return reply("OK")
	case cmd == "fake:error" || (cmd == "fake:error-after-tool" && rounds > 0):
		return Response{}, &APIError{Status: 500, Type: "api_error", Message: "fake failure"}
	case cmd == "fake:loop" || (cmd == "fake:error-after-tool" && rounds == 0):
		return call(Block{Name: "get_today", Input: json.RawMessage(`{}`)})
	case strings.HasPrefix(cmd, "fake:tool "):
		if rounds > 0 {
			return reply("Results: " + summarizeResults(req.Messages[len(req.Messages)-1]))
		}
		var uses []Block
		for _, part := range strings.Split(strings.TrimPrefix(cmd, "fake:tool "), " && ") {
			name, input, _ := strings.Cut(strings.TrimSpace(part), " ")
			if strings.TrimSpace(input) == "" {
				input = "{}"
			}
			uses = append(uses, Block{Name: name, Input: json.RawMessage(input)})
		}
		return call(uses...)
	}
	return reply("Fake reply to: " + truncateRunes(cmd, 80))
}

// lastTypedText returns the last text the user typed and how many rounds of
// tool results have followed it.
func lastTypedText(msgs []Message) (string, int) {
	rounds := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "user" {
			continue
		}
		for _, b := range msgs[i].Content {
			if b.Type == "text" {
				return b.Text, rounds
			}
		}
		rounds++
	}
	return "", rounds
}

func summarizeResults(m Message) string {
	var parts []string
	for _, b := range m.Content {
		if b.Type != "tool_result" {
			continue
		}
		var r Result
		_ = json.Unmarshal([]byte(b.Content), &r)
		s := r.Status
		if r.Summary != "" {
			s += " " + r.Summary
		}
		if r.Error != "" {
			s += " (" + r.Error + ")"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "; ")
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// fakeStructured answers a forced tool call (a briefing) from the day data
// in the prompt. It deliberately includes an invented task id and an invalid
// time, which the server must drop.
func fakeStructured(req Request) (Response, error) {
	text, _ := lastTypedText(req.Messages)
	var day struct {
		Overdue  []struct{ ID uint64 } `json:"overdue_tasks"`
		DueToday []struct{ ID uint64 } `json:"due_today"`
		Events   []struct {
			ID         uint64
			Start, End string
		} `json:"events_today"`
	}
	if start, end := strings.Index(text, "<day_data>"), strings.Index(text, "</day_data>"); start >= 0 && end > start {
		_ = json.Unmarshal([]byte(text[start+len("<day_data>"):end]), &day)
	}
	var open []uint64
	for _, t := range append(day.Overdue, day.DueToday...) {
		open = append(open, t.ID)
	}
	const invented = 999999999

	var input map[string]any
	switch req.ToolChoice.Name {
	case "submit_morning_plan":
		focus := []map[string]any{{"task_id": invented, "reason": "invented"}}
		schedule := []map[string]any{
			{"start": "25:00", "end": "26:00", "kind": "break"},
			{"start": "12:30", "end": "13:00", "kind": "break", "title": "Lunch"},
			{"start": "09:00", "end": "10:00", "kind": "task", "task_id": invented},
		}
		for i, id := range open {
			focus = append(focus, map[string]any{"task_id": id, "reason": "fake reason"})
			schedule = append(schedule, map[string]any{"start": fmt.Sprintf("%02d:00", 14+i%6), "end": fmt.Sprintf("%02d:45", 14+i%6), "kind": "task", "task_id": id})
		}
		for _, e := range day.Events {
			if e.Start != "" && e.Start < e.End {
				schedule = append(schedule, map[string]any{"start": e.Start, "end": e.End, "kind": "event", "event_id": e.ID})
			}
		}
		input = map[string]any{"summary": "Fake morning plan.", "focus": focus, "schedule": schedule, "warnings": []string{"Fake warning"}}
	case "submit_evening_summary":
		unfinished := []map[string]any{{"task_id": invented, "suggestion": "tomorrow", "reason": "invented"}}
		for _, id := range open {
			unfinished = append(unfinished, map[string]any{"task_id": id, "suggestion": "tomorrow", "reason": "fake reason"})
		}
		input = map[string]any{"summary": "Fake evening summary.", "wins": []string{"Fake win"}, "unfinished": unfinished, "tomorrow": "Fake tomorrow."}
	default:
		input = map[string]any{}
	}
	raw, _ := json.Marshal(input)
	return Response{
		Content:    []Block{{Type: "tool_use", ID: "toolu_fake_brief", Name: req.ToolChoice.Name, Input: raw}},
		StopReason: "tool_use", Model: "fake", Usage: Usage{InputTokens: len(text) / 4, OutputTokens: len(raw) / 4},
	}, nil
}
