package workflow

import (
	"context"
	"strconv"
	"strings"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/services"
)

var runStatuses = []string{models.RunRunning, models.RunSucceeded, models.RunWaitingApproval, models.RunRetrying, models.RunFailed, models.RunSkipped}

// ListRunsInput holds raw query-string values.
type ListRunsInput struct {
	AutomationID string
	Status       string // comma-separated
	Page         string
	PerPage      string
}

// ListRuns returns the user's runs, newest first.
func (e Engine) ListRuns(ctx context.Context, userID uint64, in ListRunsInput) ([]models.WorkflowRun, services.PageMeta, error) {
	fields := map[string]string{}
	var f repository.WorkflowRunFilter
	if in.AutomationID != "" {
		id, err := strconv.ParseUint(in.AutomationID, 10, 64)
		if err != nil {
			fields["automation_id"] = "must be an ID"
		}
		f.AutomationID = &id
	}
	if in.Status != "" {
		for _, s := range strings.Split(in.Status, ",") {
			if !contains(runStatuses, s) {
				fields["status"] = "must be one of: " + strings.Join(runStatuses, ", ")
				break
			}
			f.Statuses = append(f.Statuses, s)
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
	runs, total, err := repository.WorkflowRunRepository{DB: e.DB}.List(ctx, userID, f)
	if err != nil {
		return nil, meta, err
	}
	for i := range runs {
		if runs[i], err = e.Refresh(ctx, runs[i]); err != nil {
			return nil, meta, err
		}
	}
	meta.Total = total
	return runs, meta, nil
}

// GetRun returns one of the user's runs.
func (e Engine) GetRun(ctx context.Context, userID, id uint64) (models.WorkflowRun, error) {
	run, err := repository.WorkflowRunRepository{DB: e.DB}.Get(ctx, userID, id)
	if err != nil {
		return run, err
	}
	return e.Refresh(ctx, run)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
