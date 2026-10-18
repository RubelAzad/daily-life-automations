package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lifeos-ai/backend/internal/models"
)

func TestRedactJSON(t *testing.T) {
	in := `{"title":"Pay with card 4111 1111 1111 1111","password":"hunter2","nested":{"api_key":"abc","Access-Token":"x"},
		"pinned":true,"tags":["sk-ant-api03-abcdefghijk"],"order":"1234567890123","count":12}`
	got := string(RedactJSON(json.RawMessage(in)))
	for _, leak := range []string{"4111", "hunter2", `"abc"`, "sk-ant-api03"} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q in %s", leak, got)
		}
	}
	for _, keep := range []string{`"pinned":true`, `"order":"1234567890123"`, `"count":12`, "[card number removed]", "[key removed]"} {
		if !strings.Contains(got, keep) {
			t.Errorf("missing %q in %s", keep, got)
		}
	}
	if RedactJSON(nil) != nil {
		t.Error("empty input should stay empty")
	}
	if got := string(RedactJSON(json.RawMessage(`not json 4111111111111111`))); !strings.Contains(got, "card number removed") {
		t.Errorf("invalid JSON should be stored as redacted text, got %s", got)
	}
	big := `{"body":"` + strings.Repeat("a", maxStoredJSON) + `"}`
	if got := string(RedactJSON(json.RawMessage(big))); !strings.Contains(got, `"truncated":true`) {
		t.Error("large values should be truncated")
	}
}

func TestNeedsApproval(t *testing.T) {
	cases := []struct {
		risk Risk
		mode string
		want bool
	}{
		{RiskRead, models.ApprovalAlways, false},
		{RiskRead, models.ApprovalHighImpactOnly, false},
		{RiskLow, models.ApprovalAlways, true},
		{RiskLow, models.ApprovalHighImpactOnly, false},
		{RiskLow, "something-else", true}, // unknown mode: strictest
		{RiskHigh, models.ApprovalAlways, true},
		{RiskHigh, models.ApprovalHighImpactOnly, true}, // never skippable
		{Risk("unknown"), models.ApprovalHighImpactOnly, true},
	}
	for _, c := range cases {
		if got := NeedsApproval(c.risk, c.mode); got != c.want {
			t.Errorf("%s/%s: got %v, want %v", c.risk, c.mode, got, c.want)
		}
	}
}

func TestDecodeStrict(t *testing.T) {
	var v struct {
		TaskID uint64 `json:"task_id"`
	}
	if err := decodeStrict(json.RawMessage(`{"task_id":5}`), &v); err != nil || v.TaskID != 5 {
		t.Fatalf("got %v %v", v, err)
	}
	for _, bad := range []string{`{"task_id":5,"user_id":2}`, `{"task_id":"5"}`, `{"task_id":5}{"x":1}`, `[1]`, `{`} {
		var ie *InputError
		if err := decodeStrict(json.RawMessage(bad), &v); !errors.As(err, &ie) {
			t.Errorf("%s: want InputError, got %v", bad, err)
		}
	}
	if err := decodeStrict(nil, &v); err != nil {
		t.Errorf("empty input means {}: %v", err)
	}
}

// The approvals inbox approves with the assistant's tools, so every
// automation tool that can wait for approval must be one of them.
func TestApprovableAutomationToolsAreBuiltin(t *testing.T) {
	builtin := BuiltinTools(Deps{})
	for _, tool := range AutomationTools(Deps{}).All() {
		if tool.Risk == RiskRead {
			continue
		}
		b, ok := builtin.Get(tool.Name)
		if !ok || b.Risk != tool.Risk {
			t.Errorf("%s can need approval but isn't a built-in tool with the same risk", tool.Name)
		}
	}
}

func TestBuiltinToolsAllowlist(t *testing.T) {
	reg := BuiltinTools(Deps{})
	if len(reg.All()) != 18 {
		t.Errorf("got %d tools", len(reg.All()))
	}
	for _, tool := range reg.All() {
		schema := string(tool.Schema)
		if !strings.Contains(schema, `"additionalProperties":false`) {
			t.Errorf("%s: schema must reject unknown fields", tool.Name)
		}
		if strings.Contains(schema, "user_id") {
			t.Errorf("%s: the model must never choose the user", tool.Name)
		}
		if tool.Risk != RiskRead && tool.Risk != RiskLow && tool.Risk != RiskHigh {
			t.Errorf("%s: bad risk %q", tool.Name, tool.Risk)
		}
	}
	if tool, _ := reg.Get("delete_task"); tool.Risk != RiskHigh {
		t.Error("deleting data must be high risk")
	}
	if _, ok := reg.Get("run_sql"); ok {
		t.Error("unexpected tool")
	}
	defer func() {
		if recover() == nil {
			t.Error("duplicate tool names must panic")
		}
	}()
	NewRegistry(getTodayTool(Deps{}), getTodayTool(Deps{}))
}

func TestAnthropicClient(t *testing.T) {
	const key = "sk-ant-test-secret-key-123"
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != key || r.Header.Get("anthropic-version") != anthropicVersion {
			t.Errorf("bad request: %s %v", r.URL.Path, r.Header)
		}
		body, _ := io.ReadAll(r.Body)
		var req anthropicRequest
		if err := json.Unmarshal(body, &req); err != nil || req.Model != "claude-test" || req.MaxTokens != 100 || len(req.Tools) != 1 {
			t.Errorf("bad body: %s", body)
		}
		if calls == 1 { // overloaded once, then succeed
			w.Header().Set("retry-after", "1")
			w.WriteHeader(529)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"model":"claude-test","stop_reason":"tool_use","usage":{"input_tokens":12,"output_tokens":7},
			"content":[{"type":"text","text":"Let me check."},{"type":"tool_use","id":"tu_1","name":"get_today","input":{}}]}`))
	}))
	defer srv.Close()

	var slept []time.Duration
	c := AnthropicClient{APIKey: key, ModelName: "claude-test", BaseURL: srv.URL, MaxTokens: 100, sleep: func(d time.Duration) { slept = append(slept, d) }}
	resp, err := c.Complete(context.Background(), Request{
		Messages: []Message{UserText("hi")},
		Tools:    []ToolSpec{{Name: "get_today", Description: "d", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(slept) != 1 || slept[0] != time.Second {
		t.Errorf("retry: calls=%d slept=%v", calls, slept)
	}
	if resp.StopReason != "tool_use" || resp.Text() != "Let me check." || resp.Content[1].Name != "get_today" || resp.Usage.OutputTokens != 7 {
		t.Errorf("bad response: %+v", resp)
	}

	// Errors are parsed, not retried when permanent, and never contain the key.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}))
	defer bad.Close()
	calls = 0
	c.BaseURL = bad.URL
	_, err = c.Complete(context.Background(), Request{Messages: []Message{UserText("hi")}})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 401 || apiErr.Type != "authentication_error" || calls != 1 {
		t.Errorf("got %v after %d calls", err, calls)
	}
	if strings.Contains(err.Error(), key) {
		t.Error("error message leaks the API key")
	}

	if _, err := (AnthropicClient{}).Complete(context.Background(), Request{}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("no key: got %v", err)
	}
}

func TestPingLimiter(t *testing.T) {
	l := &pingLimiter{last: map[uint64]time.Time{}}
	now := time.Now()
	if !l.allow(1, now) || l.allow(1, now.Add(5*time.Second)) || !l.allow(2, now) || !l.allow(1, now.Add(11*time.Second)) {
		t.Error("limiter: one ping per user per 10 s")
	}
}
