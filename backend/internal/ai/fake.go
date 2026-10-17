package ai

import (
	"context"
	"strings"
)

// FakeClient is an offline stand-in for local development and tests
// (AI_PROVIDER=fake, allowed only with APP_ENV=local). It makes no network
// calls and costs nothing.
type FakeClient struct{}

func (FakeClient) Model() string    { return "fake" }
func (FakeClient) Provider() string { return "fake" }

func (FakeClient) Complete(_ context.Context, req Request) (Response, error) {
	var last string
	for _, m := range req.Messages {
		for _, b := range m.Content {
			if b.Type == "text" {
				last = b.Text
			}
		}
	}
	reply := "OK"
	if !strings.Contains(strings.ToLower(last), "reply with the single word ok") {
		reply = "Fake reply to: " + truncateRunes(last, 80)
	}
	return Response{
		Content:    []Block{{Type: "text", Text: reply}},
		StopReason: "end_turn",
		Model:      "fake",
		Usage:      Usage{InputTokens: len(last) / 4, OutputTokens: len(reply) / 4},
	}, nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
