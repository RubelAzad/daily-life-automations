package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	anthropicVersion = "2023-06-01"
	maxResponseBytes = 4 << 20
	defaultRetries   = 2
	maxRetryWait     = 10 * time.Second
)

// AnthropicClient calls the Claude Messages API over HTTPS.
//
// Requests and replies are never logged here; callers record only metadata
// (model, token counts, duration) in the audit log. Error messages never
// include the API key or the request body.
type AnthropicClient struct {
	APIKey    string
	ModelName string
	BaseURL   string // https://api.anthropic.com
	MaxTokens int
	HTTP      *http.Client
	Retries   int                 // retries for 429/5xx/529; 0 uses the default, -1 disables
	sleep     func(time.Duration) // overridable for tests
}

func (c AnthropicClient) Model() string    { return c.ModelName }
func (c AnthropicClient) Provider() string { return "anthropic" }

// APIError is an error response from the API.
type APIError struct {
	Status  int
	Type    string // e.g. "rate_limit_error", "overloaded_error", "invalid_request_error"
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("anthropic API %d %s: %s", e.Status, e.Type, e.Message)
}

// Retryable reports whether trying again later might succeed.
func (e *APIError) Retryable() bool {
	return e.Status == http.StatusTooManyRequests || e.Status >= 500
}

type anthropicRequest struct {
	Model     string     `json:"model"`
	MaxTokens int        `json:"max_tokens"`
	System    string     `json:"system,omitempty"`
	Messages  []Message  `json:"messages"`
	Tools     []ToolSpec `json:"tools,omitempty"`
}

type anthropicResponse struct {
	Model      string  `json:"model"`
	Content    []Block `json:"content"`
	StopReason string  `json:"stop_reason"`
	Usage      Usage   `json:"usage"`
}

type anthropicError struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c AnthropicClient) Complete(ctx context.Context, req Request) (Response, error) {
	if c.APIKey == "" {
		return Response{}, ErrNotConfigured
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = c.MaxTokens
	}
	body, err := json.Marshal(anthropicRequest{
		Model: c.ModelName, MaxTokens: maxTokens, System: req.System, Messages: req.Messages, Tools: req.Tools,
	})
	if err != nil {
		return Response{}, err
	}

	retries := c.Retries
	switch {
	case retries == 0:
		retries = defaultRetries
	case retries < 0:
		retries = 0
	}
	for attempt := 0; ; attempt++ {
		resp, wait, err := c.send(ctx, body)
		apiErr, ok := err.(*APIError)
		if err == nil || !ok || !apiErr.Retryable() || attempt >= retries {
			return resp, err
		}
		if wait <= 0 {
			wait = time.Duration(1<<attempt) * time.Second
		}
		if wait > maxRetryWait {
			wait = maxRetryWait
		}
		if c.sleep != nil {
			c.sleep(wait)
			continue
		}
		select {
		case <-ctx.Done():
			return Response{}, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// send makes one HTTP request. wait is the server's retry-after hint.
func (c AnthropicClient) send(ctx context.Context, body []byte) (Response, time.Duration, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return Response{}, 0, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", c.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(httpReq)
	if err != nil {
		// net/http errors include the URL but never headers, so no key leaks.
		return Response{}, 0, fmt.Errorf("anthropic API request failed: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return Response{}, 0, fmt.Errorf("anthropic API: reading response: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		apiErr := &APIError{Status: res.StatusCode, Type: "http_error", Message: http.StatusText(res.StatusCode)}
		var parsed anthropicError
		if json.Unmarshal(raw, &parsed) == nil && parsed.Error.Type != "" {
			apiErr.Type, apiErr.Message = parsed.Error.Type, parsed.Error.Message
		}
		var wait time.Duration
		if s, err := strconv.Atoi(res.Header.Get("retry-after")); err == nil && s > 0 {
			wait = time.Duration(s) * time.Second
		}
		return Response{}, wait, apiErr
	}

	var parsed anthropicResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Response{}, 0, fmt.Errorf("anthropic API: invalid response: %w", err)
	}
	return Response{Content: parsed.Content, StopReason: parsed.StopReason, Model: parsed.Model, Usage: parsed.Usage}, 0, nil
}

// newHTTPClient bounds each request, including reading the reply.
func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}
