package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/services"
)

// ApprovalTTL is how long a proposed action waits for approval. After that
// it expires, so an old request can't be approved by accident.
const ApprovalTTL = 24 * time.Hour

var (
	ErrNotPending      = errors.New("this action is no longer waiting for approval")
	ErrApprovalExpired = errors.New("this action expired; ask the assistant again")
)

// Executor runs tool calls for one user, applying the approval policy and
// writing every call to the audit log.
type Executor struct {
	Tools    *Registry
	Actions  repository.AIActionRepository
	Settings repository.SettingsRepository
	Log      *log.Logger
	Now      func() time.Time // overridable for tests
}

// Origin says where a tool call came from, for the audit log.
type Origin struct {
	Source         string // chat | automation | test
	ConversationID *uint64
}

// Result is what the assistant learns about a tool call.
type Result struct {
	ActionID uint64 `json:"action_id"`
	Status   string `json:"status"` // succeeded | failed | pending_approval | rejected
	Summary  string `json:"summary,omitempty"`
	Output   any    `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (e Executor) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

// Propose handles a tool call requested by the assistant. Read-only tools
// run at once; changes run at once or wait for approval depending on their
// risk and the user's ai_approval_mode. The returned error is only for
// internal failures; tool and input errors are reported in the Result.
func (e Executor) Propose(ctx context.Context, userID uint64, origin Origin, name string, input json.RawMessage) (Result, error) {
	action := models.AIAction{
		UserID: userID, Kind: models.AIActionTool, Source: origin.Source, ConversationID: origin.ConversationID,
		Tool: strPtr(truncateRunes(name, 64)), Input: RedactJSON(input),
	}

	tool, ok := e.Tools.Get(name)
	if !ok {
		action.Status, action.Error = models.AIStatusRejected, strPtr("unknown tool")
		return e.record(ctx, action, nil)
	}
	action.Risk = strPtr(string(tool.Risk))

	prep, err := tool.prepare(ctx, userID, input)
	if err != nil {
		action.Status, action.Error = models.AIStatusFailed, strPtr(e.errorText(err))
		return e.record(ctx, action, nil)
	}
	action.Summary = strPtr(truncateRunes(prep.summary, 500))

	settings, err := e.Settings.Get(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	if NeedsApproval(tool.Risk, settings.AIApprovalMode) {
		action.Status = models.AIStatusPendingApproval
		action.PendingInput = input
		if len(action.PendingInput) == 0 {
			action.PendingInput = json.RawMessage(`{}`)
		}
		return e.record(ctx, action, nil)
	}

	output := e.run(ctx, prep, &action)
	return e.record(ctx, action, output)
}

// Approve runs a pending action. It runs at most once, and only within
// ApprovalTTL of being proposed. The user's current approval mode doesn't
// matter: approving is the explicit consent.
func (e Executor) Approve(ctx context.Context, userID, id uint64) (models.AIAction, error) {
	action, err := e.claim(ctx, userID, id)
	if err != nil {
		return action, err
	}
	tool, ok := e.Tools.Get(deref(action.Tool))
	var prep prepared
	if !ok {
		err = &InputError{Message: "this tool is no longer available"}
	} else {
		prep, err = tool.prepare(ctx, userID, action.PendingInput)
	}
	if err != nil {
		action.Status, action.Error = models.AIStatusFailed, strPtr(e.errorText(err))
		now := e.now()
		action.ExecutedAt = &now
	} else {
		action.Output = encodeOutput(e.run(ctx, prep, &action))
	}
	if err := e.Actions.Finish(ctx, action); err != nil {
		return action, err
	}
	return e.Actions.Get(ctx, userID, id)
}

// Reject discards a pending action without running it.
func (e Executor) Reject(ctx context.Context, userID, id uint64) (models.AIAction, error) {
	ok, err := e.Actions.Decide(ctx, userID, id, models.AIStatusRejected, e.now())
	if err != nil {
		return models.AIAction{}, err
	}
	if !ok {
		return e.notPending(ctx, userID, id)
	}
	return e.Actions.Get(ctx, userID, id)
}

func (e Executor) claim(ctx context.Context, userID, id uint64) (models.AIAction, error) {
	now := e.now()
	ok, err := e.Actions.ClaimPending(ctx, userID, id, now.Add(-ApprovalTTL), now)
	if err != nil {
		return models.AIAction{}, err
	}
	if !ok {
		return e.notPending(ctx, userID, id)
	}
	return e.Actions.Get(ctx, userID, id)
}

// notPending explains why an approve/reject didn't apply: not found, expired
// (and marks it so), or already decided.
func (e Executor) notPending(ctx context.Context, userID, id uint64) (models.AIAction, error) {
	a, err := e.Actions.Get(ctx, userID, id)
	if err != nil {
		return a, err // ErrNotFound for other users' actions too
	}
	if a.Status == models.AIStatusPendingApproval && a.CreatedAt.Before(e.now().Add(-ApprovalTTL)) {
		if _, err := e.Actions.Decide(ctx, userID, id, models.AIStatusExpired, e.now()); err != nil {
			return a, err
		}
		return a, ErrApprovalExpired
	}
	return a, ErrNotPending
}

// run executes a prepared call and fills in the action's outcome.
func (e Executor) run(ctx context.Context, prep prepared, action *models.AIAction) (output any) {
	start := e.now()
	defer func() {
		if p := recover(); p != nil {
			e.logf("ai: tool %s panicked: %v", deref(action.Tool), p)
			action.Status, action.Error, output = models.AIStatusFailed, strPtr("internal error"), nil
		}
		ms := int(e.now().Sub(start) / time.Millisecond)
		now := e.now()
		action.DurationMS, action.ExecutedAt = &ms, &now
	}()
	out, err := prep.run(ctx)
	if err != nil {
		action.Status, action.Error = models.AIStatusFailed, strPtr(e.errorText(err))
		return nil
	}
	action.Status = models.AIStatusSucceeded
	return out
}

func (e Executor) record(ctx context.Context, action models.AIAction, output any) (Result, error) {
	action.Output = encodeOutput(output)
	id, err := e.Actions.Create(ctx, action)
	if err != nil {
		return Result{}, err
	}
	return Result{ActionID: id, Status: action.Status, Summary: deref(action.Summary), Output: output, Error: deref(action.Error)}, nil
}

// errorText turns an error into a message safe to show the user and the
// model. Unexpected errors are logged and reported generically.
func (e Executor) errorText(err error) string {
	var ve *services.ValidationError
	var ie *InputError
	switch {
	case errors.As(err, &ve):
		keys := make([]string, 0, len(ve.Fields))
		for k := range ve.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + " " + ve.Fields[k]
		}
		return truncateRunes("invalid input: "+strings.Join(parts, "; "), 1000)
	case errors.As(err, &ie):
		return truncateRunes(ie.Message, 1000)
	case errors.Is(err, repository.ErrNotFound):
		return "not found"
	default:
		e.logf("ai: tool error: %v", err)
		return "internal error"
	}
}

func (e Executor) logf(format string, args ...any) {
	if e.Log != nil {
		e.Log.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

func encodeOutput(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		b, _ = json.Marshal(fmt.Sprintf("%v", v))
	}
	return RedactJSON(b)
}

func strPtr(s string) *string { return &s }

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
