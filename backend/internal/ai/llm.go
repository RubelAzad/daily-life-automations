// Package ai holds the LLM client, the allowlist of tools the assistant may
// call, the approval policy, and the audit log of everything it does.
//
// The model never touches the database, the shell or the network directly:
// it can only ask for one of the registered tools, with input that is
// decoded strictly into a typed struct and then validated by the same
// services the REST API uses. The user is always the signed-in user, never a
// value the model supplies.
package ai

import (
	"context"
	"encoding/json"
	"errors"
)

// ErrNotConfigured means no LLM provider is set up (e.g. no API key).
var ErrNotConfigured = errors.New("AI is not configured: set ANTHROPIC_API_KEY in backend/.env")

// Client sends one request to the model and returns its reply.
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
	Model() string
	Provider() string
}

type Request struct {
	System    string
	Messages  []Message
	Tools     []ToolSpec
	MaxTokens int // 0 uses the client's default
}

type Message struct {
	Role    string  `json:"role"` // "user" or "assistant"
	Content []Block `json:"content"`
}

// Block is one piece of message content: text, a tool call from the model
// (tool_use), or the result of a tool call sent back (tool_result).
type Block struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

// ToolSpec is how a tool is described to the model.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type Response struct {
	Content    []Block
	StopReason string // "end_turn", "tool_use", "max_tokens", ...
	Model      string
	Usage      Usage
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Text joins the response's text blocks.
func (r Response) Text() string {
	var out string
	for _, b := range r.Content {
		if b.Type == "text" {
			out += b.Text
		}
	}
	return out
}

// UserText is a user message with a single text block.
func UserText(text string) Message {
	return Message{Role: "user", Content: []Block{{Type: "text", Text: text}}}
}
