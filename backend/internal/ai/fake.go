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
