//go:build integration

package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
)

// recordingClient wraps FakeClient and keeps every request.
type recordingClient struct {
	FakeClient
	mu   sync.Mutex
	reqs []Request
}

func (c *recordingClient) Complete(ctx context.Context, req Request) (Response, error) {
	c.mu.Lock()
	c.reqs = append(c.reqs, req)
	c.mu.Unlock()
	return c.FakeClient.Complete(ctx, req)
}

func (c *recordingClient) last() Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reqs[len(c.reqs)-1]
}

func chatFixture(t *testing.T) (fixture, Service, *recordingClient, models.AIConversation) {
	t.Helper()
	f := newFixture(t)
	client := &recordingClient{}
	svc := NewService(client, f.exec, repository.AIConversationRepository{DB: testDB}, repository.UserRepository{DB: testDB})
	conv, err := svc.CreateConversation(f.ctx, f.user, "")
	if err != nil {
		t.Fatal(err)
	}
	return f, svc, client, conv
}

// assertWellFormed checks the history is valid for the Messages API: it
// starts with the user, roles alternate, and every tool_use is answered by a
// tool_result with the same id in the next message.
func assertWellFormed(t *testing.T, svc Service, f fixture, convID uint64) []Message {
	t.Helper()
	stored, err := svc.Conversations.Messages(f.ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := toModelMessages(stored)
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range msgs {
		want := "user"
		if i%2 == 1 {
			want = "assistant"
		}
		if m.Role != want {
			t.Fatalf("message %d: role %s, want %s", i, m.Role, want)
		}
		uses := toolUses(m)
		if len(uses) == 0 {
			continue
		}
		if i+1 >= len(msgs) {
			t.Fatalf("message %d: tool_use without a result", i)
		}
		for j, u := range uses {
			if r := msgs[i+1].Content[j]; r.Type != "tool_result" || r.ToolUseID != u.ID {
				t.Fatalf("message %d: tool_use %s answered by %+v", i, u.ID, r)
			}
		}
	}
	if len(msgs) > 0 && msgs[len(msgs)-1].Role != "assistant" {
		t.Error("a finished turn must end with the assistant")
	}
	return msgs
}

func TestChatTurnWithApproval(t *testing.T) {
	f, svc, client, conv := chatFixture(t)
	res, err := svc.SendMessage(f.ctx, f.user, conv.ID, `fake:tool create_task {"title":"Buy milk"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Messages) != 4 || len(res.Actions) != 1 {
		t.Fatalf("got %d messages, %d actions", len(res.Messages), len(res.Actions))
	}
	a := res.Actions[0]
	if a.Status != models.AIStatusPendingApproval || a.ConversationID == nil || *a.ConversationID != conv.ID || a.ExpiresAt == nil {
		t.Errorf("action: %+v", a)
	}
	if !strings.Contains(string(res.Messages[3].Content), "Results: pending_approval Create task “Buy milk”") {
		t.Errorf("final reply: %s", res.Messages[3].Content)
	}
	assertWellFormed(t, svc, f, conv.ID)

	req := client.last()
	if !req.Cache || req.System != systemRules || len(req.Tools) != 14 {
		t.Error("request must cache the rules and send the tool list")
	}
	if !strings.Contains(req.SystemSuffix, "Approval setting: Changes") || !strings.Contains(req.SystemSuffix, "time zone UTC") {
		t.Errorf("context: %s", req.SystemSuffix)
	}
	got, _ := svc.Conversations.Get(f.ctx, f.user, conv.ID)
	if got.Title != `fake:tool create_task {"title":"Buy milk"}` {
		t.Errorf("title: %q", got.Title)
	}

	// The next turn tells the model what the user decided, and sends the history.
	if _, err := svc.Approve(f.ctx, f.user, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SendMessage(f.ctx, f.user, conv.ID, "thanks"); err != nil {
		t.Fatal(err)
	}
	req = client.last()
	if !strings.Contains(req.SystemSuffix, "Create task “Buy milk”: approved, and it succeeded") {
		t.Errorf("decision not reported: %s", req.SystemSuffix)
	}
	if len(req.Messages) != 5 {
		t.Errorf("history: got %d messages, want 5", len(req.Messages))
	}
	if _, err := svc.SendMessage(f.ctx, f.user, conv.ID, "again"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(client.last().SystemSuffix, "Buy milk") {
		t.Error("a decision is reported only once")
	}
	assertWellFormed(t, svc, f, conv.ID)
}

func TestChatStepLimit(t *testing.T) {
	f, svc, client, conv := chatFixture(t)
	res, err := svc.SendMessage(f.ctx, f.user, conv.ID, "fake:loop")
	if err != nil {
		t.Fatal(err)
	}
	if len(client.reqs) != maxChatSteps || len(res.Actions) != maxChatSteps {
		t.Errorf("model calls %d, actions %d", len(client.reqs), len(res.Actions))
	}
	last := res.Messages[len(res.Messages)-1]
	if last.Role != "assistant" || !strings.Contains(string(last.Content), "I stopped after 8 steps") {
		t.Errorf("last: %s", last.Content)
	}
	assertWellFormed(t, svc, f, conv.ID)
	_, calls, _ := f.exec.Actions.List(f.ctx, f.user, repository.AIActionFilter{Kind: models.AIActionLLMCall, ConversationID: &conv.ID, Limit: 50})
	if calls != maxChatSteps {
		t.Errorf("model calls audited: %d", calls)
	}
}

func TestChatModelErrors(t *testing.T) {
	f, svc, _, conv := chatFixture(t)
	_, err := svc.SendMessage(f.ctx, f.user, conv.ID, "fake:error")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("got %v", err)
	}
	if msgs, _ := svc.Conversations.Messages(f.ctx, conv.ID); len(msgs) != 0 {
		t.Errorf("failed first call must leave no messages, got %d", len(msgs))
	}
	failed, _, _ := f.exec.Actions.List(f.ctx, f.user, repository.AIActionFilter{Statuses: []string{models.AIStatusFailed}, Kind: models.AIActionLLMCall, Limit: 5})
	if len(failed) != 1 || !strings.Contains(deref(failed[0].Error), "fake failure") {
		t.Errorf("failed call must be audited: %+v", failed)
	}

	res, err := svc.SendMessage(f.ctx, f.user, conv.ID, "fake:error-after-tool")
	if err != nil {
		t.Fatal(err)
	}
	last := res.Messages[len(res.Messages)-1]
	if !strings.Contains(string(last.Content), "couldn't finish because the AI service returned an error") || len(res.Actions) != 1 {
		t.Errorf("got %s, %d actions", last.Content, len(res.Actions))
	}
	assertWellFormed(t, svc, f, conv.ID)
}

func TestChatValidationAndOwnership(t *testing.T) {
	f, svc, _, conv := chatFixture(t)
	other := newFixture(t)
	if _, err := svc.SendMessage(other.ctx, other.user, conv.ID, "hi"); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("other user: %v", err)
	}
	if _, err := svc.GetConversation(other.ctx, other.user, conv.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("other user read: %v", err)
	}
	for _, text := range []string{"   ", strings.Repeat("a", maxMessageChars+1)} {
		if _, err := svc.SendMessage(f.ctx, f.user, conv.ID, text); err == nil || !strings.Contains(err.Error(), "validation") {
			t.Errorf("%d chars: %v", len(text), err)
		}
	}

	// Deleting a chat keeps its audit log.
	if _, err := svc.SendMessage(f.ctx, f.user, conv.ID, "fake:tool get_today"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteConversation(f.ctx, f.user, conv.ID); err != nil {
		t.Fatal(err)
	}
	_, total, _ := f.exec.Actions.List(f.ctx, f.user, repository.AIActionFilter{Limit: 50})
	if total < 3 {
		t.Errorf("audit entries kept: %d", total)
	}
	var n int
	_ = testDB.QueryRow(`SELECT COUNT(*) FROM ai_messages WHERE conversation_id = ?`, conv.ID).Scan(&n)
	if n != 0 {
		t.Errorf("messages left: %d", n)
	}
	raw, _ := json.Marshal(conv.ID)
	_ = raw
}
