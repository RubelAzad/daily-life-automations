package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"lifeos-ai/backend/internal/models"
)

func msg(id uint64, role string, blocks ...Block) models.AIMessage {
	b, _ := json.Marshal(blocks)
	return models.AIMessage{ID: id, Role: role, Content: b}
}

func TestTrimHistoryKeepsToolPairs(t *testing.T) {
	var h []models.AIMessage
	for i := 0; len(h) < 60; i++ {
		h = append(h,
			msg(uint64(len(h)+1), "user", Block{Type: "text", Text: fmt.Sprint("q", i)}),
			msg(uint64(len(h)+2), "assistant", Block{Type: "tool_use", ID: "t", Name: "get_today"}),
			msg(uint64(len(h)+3), "user", Block{Type: "tool_result", ToolUseID: "t", Content: "{}"}),
			msg(uint64(len(h)+4), "assistant", Block{Type: "text", Text: "a"}),
		)
	}
	got := trimHistory(h)
	if len(got) > maxHistoryMessages || len(got) == 0 {
		t.Fatalf("got %d messages", len(got))
	}
	if got[0].Role != "user" || !hasTypedText(got[0].Content) {
		t.Errorf("must start at a typed user message, got %s %s", got[0].Role, got[0].Content)
	}
	if short := h[:8]; len(trimHistory(short)) != 8 {
		t.Error("short histories are kept whole")
	}
}

func TestCleanReply(t *testing.T) {
	use := Block{Type: "tool_use", ID: "x", Name: "get_today"}
	m := cleanReply(Response{StopReason: "tool_use", Content: []Block{{Type: "text", Text: "  "}, use}})
	if len(m.Content) != 1 || m.Content[0].Type != "tool_use" || string(m.Content[0].Input) != "{}" {
		t.Errorf("tool_use: %+v", m.Content)
	}
	m = cleanReply(Response{StopReason: "max_tokens", Content: []Block{{Type: "text", Text: "Partial"}, use}})
	if len(m.Content) != 1 || m.Content[0].Text != "Partial" {
		t.Errorf("cut-off tool calls must be dropped: %+v", m.Content)
	}
	m = cleanReply(Response{StopReason: "end_turn"})
	if m.Content[0].Text != "(no reply)" {
		t.Errorf("empty reply: %+v", m.Content)
	}
}

func TestToolResultBlock(t *testing.T) {
	b := toolResultBlock("tu1", Result{ActionID: 3, Status: models.AIStatusFailed, Error: "not found"})
	if b.ToolUseID != "tu1" || !b.IsError || !strings.Contains(b.Content, `"error":"not found"`) {
		t.Errorf("got %+v", b)
	}
	if b := toolResultBlock("tu2", Result{Status: models.AIStatusPendingApproval}); b.IsError {
		t.Error("pending is not an error")
	}
	big := toolResultBlock("tu3", Result{Status: "succeeded", Output: strings.Repeat("x", maxToolResultChars*2)})
	if !strings.HasSuffix(big.Content, "[result truncated; narrow the request]") {
		t.Error("large results must be truncated")
	}
}

func TestBuildRequestCaching(t *testing.T) {
	tools := []ToolSpec{{Name: "a", InputSchema: json.RawMessage(`{}`)}, {Name: "b", InputSchema: json.RawMessage(`{}`)}}
	r := buildRequest("m", 10, Request{System: "rules", SystemSuffix: "now", Cache: true, Tools: tools})
	if len(r.System) != 2 || r.System[0].CacheControl == nil || r.System[1].CacheControl != nil {
		t.Errorf("system: %+v", r.System)
	}
	if r.Tools[0].CacheControl != nil || r.Tools[1].CacheControl == nil {
		t.Error("only the last tool is a cache breakpoint")
	}
	body, _ := json.Marshal(r)
	if !strings.Contains(string(body), `"cache_control":{"type":"ephemeral"}`) || !strings.Contains(string(body), `"name":"b","description":"","input_schema":{}`) {
		t.Errorf("body: %s", body)
	}
	r = buildRequest("m", 10, Request{System: "rules", Tools: tools})
	if r.System[0].CacheControl != nil || r.Tools[1].CacheControl != nil {
		t.Error("no cache marks unless asked")
	}
}

func TestChatState(t *testing.T) {
	c := newChatState()
	now := time.Now()
	if err := c.begin(1, 10, now); err != nil {
		t.Fatal(err)
	}
	if err := c.begin(1, 10, now); !errors.Is(err, ErrChatBusy) {
		t.Errorf("same conversation twice: %v", err)
	}
	c.end(10)
	for i := 1; i < chatTurnLimit; i++ {
		if err := c.begin(1, uint64(100+i), now); err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
	}
	if err := c.begin(1, 999, now); !errors.Is(err, ErrChatRateLimited) {
		t.Errorf("limit: %v", err)
	}
	if err := c.begin(2, 998, now); err != nil {
		t.Errorf("other users unaffected: %v", err)
	}
	if err := c.begin(1, 997, now.Add(chatTurnWindow+time.Second)); err != nil {
		t.Errorf("window passes: %v", err)
	}
}

func TestTitleFrom(t *testing.T) {
	if got := titleFrom("  Plan my week\nwith details"); got != "Plan my week" {
		t.Errorf("got %q", got)
	}
	if got := titleFrom(strings.Repeat("a", 100)); len([]rune(got)) != maxTitleChars+1 {
		t.Errorf("got %d runes", len([]rune(got)))
	}
}

func TestFakeClientScripts(t *testing.T) {
	ctx := context.Background()
	r, _ := FakeClient{}.Complete(ctx, Request{Messages: []Message{UserText(`fake:tool create_task {"title":"Milk"} && get_today`)}})
	if r.StopReason != "tool_use" || len(toolUses(Message{Content: r.Content})) != 2 || string(r.Content[1].Input) != `{"title":"Milk"}` {
		t.Fatalf("got %+v", r)
	}
	if _, err := (FakeClient{}).Complete(ctx, Request{Messages: []Message{UserText("fake:error")}}); err == nil {
		t.Error("fake:error should fail")
	}
	r, _ = FakeClient{}.Complete(ctx, Request{Messages: []Message{UserText("hello")}})
	if r.Text() != "Fake reply to: hello" {
		t.Errorf("got %q", r.Text())
	}
}
