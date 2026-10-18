// Package approvals is the approvals inbox: actions from the assistant and
// from automations that wait for the user's decision. Approving runs an
// action through the same executor (and audit log) that proposed it; each
// decision updates the automation run that was waiting on it.
package approvals

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"lifeos-ai/backend/internal/ai"
	"lifeos-ai/backend/internal/events"
	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/scheduler"
	"lifeos-ai/backend/internal/services"
	"lifeos-ai/backend/internal/workflow"
)

// JobExpire is the worker job that expires requests nobody decided on.
const JobExpire = "approvals.expire"

const (
	// MaxBatch is the most actions one bulk decision takes.
	MaxBatch = 50
	// sweepLimit caps the requests one expiry sweep handles; the rest wait
	// for the next sweep.
	sweepLimit = 500
)

// Service is used by the API (the inbox) and the worker (expiry).
type Service struct {
	DB *sql.DB
	// Executor approves and rejects. It must have the assistant's tools,
	// which include every automation tool that can need approval.
	Executor ai.Executor
	Engine   workflow.Engine
	Log      *log.Logger
}

// Approval is an action with where it came from.
type Approval struct {
	models.AIAction
	Automation   *Ref               `json:"automation"`
	Run          *repository.RunRef `json:"run"`
	Conversation *Ref               `json:"conversation"`
}

// Ref names an automation or a chat.
type Ref struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

// Result is the outcome of one action in a bulk decision.
type Result struct {
	ID     uint64    `json:"id"`
	OK     bool      `json:"ok"`
	Error  string    `json:"error,omitempty"`
	Action *Approval `json:"action,omitempty"`
}

// PageInput holds raw query-string values.
type PageInput struct {
	Page    string
	PerPage string
}

func (s Service) now() time.Time {
	if s.Executor.Now != nil {
		return s.Executor.Now().UTC()
	}
	return time.Now().UTC()
}

// Pending lists the requests waiting for the user, the soonest to expire
// first.
func (s Service) Pending(ctx context.Context, userID uint64, in PageInput) ([]Approval, services.PageMeta, error) {
	cutoff := s.now().Add(-ai.ApprovalTTL)
	return s.list(ctx, userID, in, repository.AIActionFilter{
		Statuses: []string{models.AIStatusPendingApproval}, Kind: models.AIActionTool, CreatedAfter: &cutoff, OldestFirst: true,
	})
}

// History lists decided requests (approved, rejected or expired), the
// latest decision first.
func (s Service) History(ctx context.Context, userID uint64, in PageInput) ([]Approval, services.PageMeta, error) {
	return s.list(ctx, userID, in, repository.AIActionFilter{Decided: true})
}

// Count is the number of requests waiting, for the badge in the menu.
func (s Service) Count(ctx context.Context, userID uint64) (int, error) {
	return s.Executor.Actions.CountPending(ctx, userID, s.now().Add(-ai.ApprovalTTL))
}

// Get returns one of the user's actions with its source.
func (s Service) Get(ctx context.Context, userID, id uint64) (Approval, error) {
	a, err := s.Executor.Actions.Get(ctx, userID, id)
	if err != nil {
		return Approval{}, err
	}
	list, err := s.describe(ctx, userID, []models.AIAction{a})
	if err != nil {
		return Approval{}, err
	}
	return list[0], nil
}

// Decide approves (runs) or rejects a pending action, then brings the run
// that was waiting on it up to date. Errors are ai.ErrNotPending,
// ai.ErrApprovalExpired, repository.ErrNotFound or internal.
func (s Service) Decide(ctx context.Context, userID, id uint64, approve bool) (Approval, error) {
	a, err := s.Executor.Actions.Get(ctx, userID, id)
	if err != nil {
		return Approval{}, err
	}
	if approve && a.AutomationID != nil {
		// What the action changes publishes events as the automation's
		// doing, so it can't trigger itself and chains stay limited.
		depth := 0
		if a.WorkflowRunID != nil {
			if run, err := (repository.WorkflowRunRepository{DB: s.DB}).Get(ctx, userID, *a.WorkflowRunID); err == nil {
				depth = run.Depth
			}
		}
		ctx = events.WithCause(ctx, events.Cause{AutomationID: *a.AutomationID, Depth: depth})
	}
	if approve {
		_, err = s.Executor.Approve(ctx, userID, id)
	} else {
		_, err = s.Executor.Reject(ctx, userID, id)
	}
	if err != nil && !errors.Is(err, ai.ErrApprovalExpired) && !errors.Is(err, ai.ErrNotPending) {
		return Approval{}, err
	}
	// Expired requests are marked so by the failed attempt; the run learns
	// that too.
	if a.WorkflowRunID != nil {
		if _, rerr := s.Engine.RefreshRun(ctx, userID, *a.WorkflowRunID); rerr != nil && !errors.Is(rerr, repository.ErrNotFound) {
			return Approval{}, rerr
		}
	}
	out, gerr := s.Get(ctx, userID, id)
	if gerr != nil {
		return out, gerr
	}
	return out, err
}

// DecideMany decides up to MaxBatch actions, in the order given. One that
// can't be decided (already decided, expired, not found) doesn't stop the
// others; internal errors do.
func (s Service) DecideMany(ctx context.Context, userID uint64, ids []uint64, approve bool) ([]Result, error) {
	if len(ids) == 0 || len(ids) > MaxBatch {
		return nil, &services.ValidationError{Fields: map[string]string{"ids": fmt.Sprintf("must list 1–%d actions", MaxBatch)}}
	}
	seen := map[uint64]bool{}
	out := make([]Result, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		a, err := s.Decide(ctx, userID, id, approve)
		switch {
		case errors.Is(err, repository.ErrNotFound):
			out = append(out, Result{ID: id, Error: "not found"})
		case errors.Is(err, ai.ErrNotPending), errors.Is(err, ai.ErrApprovalExpired):
			out = append(out, Result{ID: id, Error: err.Error(), Action: &a})
		case err != nil:
			return out, err
		default:
			out = append(out, Result{ID: id, OK: true, Action: &a})
		}
	}
	return out, nil
}

// Register adds the expiry sweep to the worker's scheduler.
func (s Service) Register(sched *scheduler.Scheduler, every time.Duration) {
	sched.Every(scheduler.Repeating{Kind: JobExpire, Every: every, Handler: func(ctx context.Context, _ scheduler.Job) error {
		_, err := s.Expire(ctx)
		return err
	}})
}

// Expire marks requests older than ai.ApprovalTTL as expired, so they leave
// the inbox and the automation runs waiting on them fail. The user is told
// about each run that failed this way. It returns how many expired.
func (s Service) Expire(ctx context.Context) (int, error) {
	now := s.now()
	stale, err := s.Executor.Actions.ListStale(ctx, now.Add(-ai.ApprovalTTL), sweepLimit)
	if err != nil {
		return 0, err
	}
	type runKey struct{ user, run uint64 }
	runs := map[runKey]bool{}
	n := 0
	for _, a := range stale {
		ok, err := s.Executor.Actions.Decide(ctx, a.UserID, a.ID, models.AIStatusExpired, now)
		if err != nil {
			return n, err
		}
		if !ok {
			continue // decided meanwhile
		}
		n++
		if a.WorkflowRunID != nil {
			runs[runKey{a.UserID, *a.WorkflowRunID}] = true
		}
	}
	for k := range runs {
		run, err := s.Engine.RefreshRun(ctx, k.user, k.run)
		if errors.Is(err, repository.ErrNotFound) {
			continue
		}
		if err != nil {
			return n, err
		}
		if run.Status != models.RunFailed {
			continue
		}
		_, err = repository.NotificationRepository{DB: s.DB}.Insert(ctx, models.Notification{
			UserID: k.user, Kind: "automation", Priority: "normal",
			Title:          fmt.Sprintf("“%s” didn't finish: an approval expired", run.AutomationName),
			Body:           run.Error,
			Link:           strPtr(fmt.Sprintf("/automations?run=%d", run.ID)),
			IdempotencyKey: "workflow-run:" + strconv.FormatUint(run.ID, 10) + ":expired",
		})
		if err != nil {
			return n, err
		}
	}
	if n > 0 {
		s.logf("approvals: expired %d requests", n)
	}
	return n, nil
}

func (s Service) list(ctx context.Context, userID uint64, in PageInput, f repository.AIActionFilter) ([]Approval, services.PageMeta, error) {
	meta, err := page(in)
	if err != nil {
		return nil, meta, err
	}
	f.Limit, f.Offset = meta.PerPage, (meta.Page-1)*meta.PerPage
	actions, total, err := s.Executor.Actions.List(ctx, userID, f)
	if err != nil {
		return nil, meta, err
	}
	meta.Total = total
	out, err := s.describe(ctx, userID, actions)
	return out, meta, err
}

// describe adds each action's source and, while pending, its expiry.
func (s Service) describe(ctx context.Context, userID uint64, actions []models.AIAction) ([]Approval, error) {
	var autoIDs, runIDs, chatIDs []uint64
	for _, a := range actions {
		if a.AutomationID != nil {
			autoIDs = append(autoIDs, *a.AutomationID)
		}
		if a.WorkflowRunID != nil {
			runIDs = append(runIDs, *a.WorkflowRunID)
		}
		if a.ConversationID != nil {
			chatIDs = append(chatIDs, *a.ConversationID)
		}
	}
	repo := repository.ApprovalRepository{DB: s.DB}
	autos, err := repo.AutomationNames(ctx, userID, autoIDs)
	if err != nil {
		return nil, err
	}
	runs, err := repo.Runs(ctx, userID, runIDs)
	if err != nil {
		return nil, err
	}
	chats, err := repo.ConversationTitles(ctx, userID, chatIDs)
	if err != nil {
		return nil, err
	}
	out := make([]Approval, len(actions))
	for i, a := range actions {
		if a.Status == models.AIStatusPendingApproval {
			t := a.CreatedAt.Add(ai.ApprovalTTL)
			a.ExpiresAt = &t
		}
		out[i] = Approval{AIAction: a}
		if a.AutomationID != nil {
			if name, ok := autos[*a.AutomationID]; ok {
				out[i].Automation = &Ref{ID: *a.AutomationID, Name: name}
			}
		}
		if a.WorkflowRunID != nil {
			if ref, ok := runs[*a.WorkflowRunID]; ok {
				out[i].Run = &ref
			}
		}
		if a.ConversationID != nil {
			if title, ok := chats[*a.ConversationID]; ok {
				out[i].Conversation = &Ref{ID: *a.ConversationID, Name: title}
			}
		}
	}
	return out, nil
}

func page(in PageInput) (services.PageMeta, error) {
	meta := services.PageMeta{Page: 1, PerPage: 20}
	fields := map[string]string{}
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
		return meta, &services.ValidationError{Fields: fields}
	}
	return meta, nil
}

func (s Service) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

func strPtr(s string) *string { return &s }
