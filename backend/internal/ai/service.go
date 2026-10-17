package ai

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/services"
)

// pingInterval limits connection tests per user; each one is a paid call.
const pingInterval = 10 * time.Second

var ErrRateLimited = errors.New("please wait a few seconds before testing again")

// Service is what the HTTP handlers use.
type Service struct {
	Client        Client // nil when AI is not configured
	Executor      Executor
	Conversations repository.AIConversationRepository
	Users         repository.UserRepository
	pings         *pingLimiter
	chat          *chatState
}

func NewService(client Client, executor Executor, conversations repository.AIConversationRepository, users repository.UserRepository) Service {
	return Service{
		Client: client, Executor: executor, Conversations: conversations, Users: users,
		pings: &pingLimiter{last: map[uint64]time.Time{}},
		chat:  newChatState(),
	}
}

type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Risk        Risk   `json:"risk"`
}

type Status struct {
	Enabled  bool       `json:"enabled"`
	Provider string     `json:"provider,omitempty"`
	Model    string     `json:"model,omitempty"`
	Reason   string     `json:"reason,omitempty"` // why it's disabled
	Tools    []ToolInfo `json:"tools"`
}

func (s Service) Status() Status {
	st := Status{Tools: []ToolInfo{}}
	for _, t := range s.Executor.Tools.All() {
		st.Tools = append(st.Tools, ToolInfo{Name: t.Name, Description: t.Description, Risk: t.Risk})
	}
	if s.Client == nil {
		st.Reason = ErrNotConfigured.Error()
		return st
	}
	st.Enabled, st.Provider, st.Model = true, s.Client.Provider(), s.Client.Model()
	return st
}

type PingResult struct {
	Reply        string `json:"reply"`
	Model        string `json:"model"`
	DurationMS   int    `json:"duration_ms"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	ActionID     uint64 `json:"action_id"`
}

// Ping sends a tiny request to check the API key and connection. It is
// recorded in the audit log like every model call.
func (s Service) Ping(ctx context.Context, userID uint64) (PingResult, error) {
	if s.Client == nil {
		return PingResult{}, ErrNotConfigured
	}
	if !s.pings.allow(userID, time.Now()) {
		return PingResult{}, ErrRateLimited
	}
	start := time.Now()
	resp, err := s.Client.Complete(ctx, Request{
		System:    "You are a connectivity check for the LifeOS app.",
		Messages:  []Message{UserText("Reply with the single word OK.")},
		MaxTokens: 16,
	})
	id, recErr := s.RecordModelCall(ctx, userID, Origin{Source: "ping"}, s.Client.Model(), resp, time.Since(start), err)
	if err != nil {
		return PingResult{}, err
	}
	if recErr != nil {
		return PingResult{}, recErr
	}
	model := resp.Model
	if model == "" {
		model = s.Client.Model()
	}
	return PingResult{
		Reply: truncateRunes(strings.TrimSpace(resp.Text()), 200), Model: model, DurationMS: int(time.Since(start) / time.Millisecond),
		InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens, ActionID: id,
	}, nil
}

// RecordModelCall logs a model call's metadata (never the prompt or reply).
func (s Service) RecordModelCall(ctx context.Context, userID uint64, origin Origin, model string, resp Response, took time.Duration, callErr error) (uint64, error) {
	ms := int(took / time.Millisecond)
	// Input tokens include the cached part, so totals are comparable.
	in := resp.Usage.InputTokens + resp.Usage.CacheReadInputTokens + resp.Usage.CacheCreationInputTokens
	out := resp.Usage.OutputTokens
	now := time.Now().UTC()
	a := models.AIAction{
		UserID: userID, Kind: models.AIActionLLMCall, Source: origin.Source, ConversationID: origin.ConversationID,
		Model: strPtr(model), Status: models.AIStatusSucceeded,
		InputTokens: &in, OutputTokens: &out, DurationMS: &ms, ExecutedAt: &now,
	}
	if resp.Model != "" {
		a.Model = strPtr(resp.Model)
	}
	if callErr != nil {
		a.Status = models.AIStatusFailed
		a.Error = strPtr(truncateRunes(RedactText(callErr.Error()), 1000))
		a.InputTokens, a.OutputTokens = nil, nil
	}
	return s.Executor.Actions.Create(ctx, a)
}

type ListActionsInput struct {
	Status         string // comma-separated statuses
	Kind           string
	ConversationID string
	AutomationID   string
	Page           string
	PerPage        string
}

func (s Service) ListActions(ctx context.Context, userID uint64, in ListActionsInput) ([]models.AIAction, services.PageMeta, error) {
	fields := map[string]string{}
	f := repository.AIActionFilter{Kind: in.Kind}
	if in.Status != "" {
		for _, st := range strings.Split(in.Status, ",") {
			if !contains(models.AIActionStatuses, st) {
				fields["status"] = "must be one of: " + strings.Join(models.AIActionStatuses, ", ")
				break
			}
			f.Statuses = append(f.Statuses, st)
		}
	}
	if f.Kind != "" && f.Kind != models.AIActionTool && f.Kind != models.AIActionLLMCall {
		fields["kind"] = "must be one of: tool, llm_call"
	}
	if in.ConversationID != "" {
		id, err := strconv.ParseUint(in.ConversationID, 10, 64)
		if err != nil {
			fields["conversation_id"] = "must be an ID"
		} else {
			f.ConversationID = &id
		}
	}
	if in.AutomationID != "" {
		id, err := strconv.ParseUint(in.AutomationID, 10, 64)
		if err != nil {
			fields["automation_id"] = "must be an ID"
		} else {
			f.AutomationID = &id
		}
	}
	meta := services.PageMeta{Page: 1, PerPage: 20}
	for key, raw := range map[string]string{"page": in.Page, "per_page": in.PerPage} {
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || (key == "per_page" && n > 100) {
			fields[key] = "must be a positive integer (per_page at most 100)"
			continue
		}
		if key == "page" {
			meta.Page = n
		} else {
			meta.PerPage = n
		}
	}
	if len(fields) > 0 {
		return nil, meta, &services.ValidationError{Fields: fields}
	}
	f.Limit, f.Offset = meta.PerPage, (meta.Page-1)*meta.PerPage
	actions, total, err := s.Executor.Actions.List(ctx, userID, f)
	meta.Total = total
	for i := range actions {
		withExpiry(&actions[i])
	}
	return actions, meta, err
}

func (s Service) GetAction(ctx context.Context, userID, id uint64) (models.AIAction, error) {
	a, err := s.Executor.Actions.Get(ctx, userID, id)
	withExpiry(&a)
	return a, err
}

func (s Service) Approve(ctx context.Context, userID, id uint64) (models.AIAction, error) {
	return s.Executor.Approve(ctx, userID, id)
}

func (s Service) Reject(ctx context.Context, userID, id uint64) (models.AIAction, error) {
	return s.Executor.Reject(ctx, userID, id)
}

func withExpiry(a *models.AIAction) {
	if a.Status == models.AIStatusPendingApproval {
		t := a.CreatedAt.Add(ApprovalTTL)
		a.ExpiresAt = &t
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

type pingLimiter struct {
	mu   sync.Mutex
	last map[uint64]time.Time
}

func (l *pingLimiter) allow(userID uint64, now time.Time) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if t, ok := l.last[userID]; ok && now.Sub(t) < pingInterval {
		return false
	}
	l.last[userID] = now
	return true
}

// NewClient builds the configured client, or nil when AI is off.
func NewClient(provider, apiKey, model, baseURL string, maxTokens int, timeout time.Duration) Client {
	switch {
	case provider == "fake":
		return FakeClient{}
	case provider == "anthropic" && apiKey != "":
		return AnthropicClient{APIKey: apiKey, ModelName: model, BaseURL: baseURL, MaxTokens: maxTokens, HTTP: newHTTPClient(timeout)}
	}
	return nil
}
