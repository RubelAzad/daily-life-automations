package models

import (
	"encoding/json"
	"time"
)

const (
	AIActionTool    = "tool"
	AIActionLLMCall = "llm_call"

	AIStatusSucceeded       = "succeeded"
	AIStatusFailed          = "failed"
	AIStatusPendingApproval = "pending_approval"
	AIStatusRunning         = "running" // approved, executing now
	AIStatusRejected        = "rejected"
	AIStatusExpired         = "expired"
)

var AIActionStatuses = []string{AIStatusSucceeded, AIStatusFailed, AIStatusPendingApproval, AIStatusRunning, AIStatusRejected, AIStatusExpired}

// AIAction is one entry in the AI audit log.
type AIAction struct {
	ID     uint64 `json:"id"`
	UserID uint64 `json:"-"`
	Kind   string `json:"kind"`
	Source string `json:"source"`
	// ConversationID is the chat the action came from, if any.
	ConversationID *uint64 `json:"conversation_id"`
	// AutomationID is the automation that took the action, if any.
	AutomationID *uint64 `json:"automation_id"`
	// WorkflowRunID is the automation run that took the action, if any.
	WorkflowRunID *uint64 `json:"workflow_run_id"`
	// IdempotencyKey lets a retried automation run skip actions it took.
	IdempotencyKey *string `json:"-"`
	Tool           *string `json:"tool"`
	Risk           *string `json:"risk"`
	Summary        *string `json:"summary"`
	// Input and Output are redacted copies for display.
	Input  json.RawMessage `json:"input"`
	Output json.RawMessage `json:"output"`
	// PendingInput is the exact input, kept only while awaiting approval.
	PendingInput json.RawMessage `json:"-"`
	Status       string          `json:"status"`
	Error        *string         `json:"error"`
	Model        *string         `json:"model"`
	InputTokens  *int            `json:"input_tokens"`
	OutputTokens *int            `json:"output_tokens"`
	DurationMS   *int            `json:"duration_ms"`
	DecidedAt    *time.Time      `json:"decided_at"`
	ExecutedAt   *time.Time      `json:"executed_at"`
	// ExpiresAt is set while the action awaits approval.
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// AIConversation is a chat with the assistant.
type AIConversation struct {
	ID        uint64    `json:"id"`
	UserID    uint64    `json:"-"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Messages is set only on the detail view (a pointer so an empty chat
	// still returns "messages": []).
	Messages *[]AIMessage `json:"messages,omitempty"`
}

// AIMessage is one turn, as an array of content blocks: text, tool_use
// (assistant) and tool_result (user role, sent back by the app).
type AIMessage struct {
	ID             uint64          `json:"id"`
	ConversationID uint64          `json:"-"`
	Role           string          `json:"role"`
	Content        json.RawMessage `json:"content"`
	CreatedAt      time.Time       `json:"created_at"`
}
