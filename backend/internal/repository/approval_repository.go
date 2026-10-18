package repository

import "context"

// ApprovalRepository looks up where approval requests came from, for the
// approvals inbox.
type ApprovalRepository struct {
	DB DBTX
}

// RunRef is the part of a workflow run the inbox shows.
type RunRef struct {
	ID             uint64  `json:"id"`
	TriggerSummary *string `json:"trigger_summary"`
	Status         string  `json:"status"`
}

// AutomationNames maps the user's automation IDs to their names; deleted
// automations are missing.
func (r ApprovalRepository) AutomationNames(ctx context.Context, userID uint64, ids []uint64) (map[uint64]string, error) {
	return r.names(ctx, `SELECT id, name FROM automations WHERE user_id = ? AND id IN `, userID, ids)
}

// ConversationTitles maps the user's chat IDs to their titles.
func (r ApprovalRepository) ConversationTitles(ctx context.Context, userID uint64, ids []uint64) (map[uint64]string, error) {
	return r.names(ctx, `SELECT id, title FROM ai_conversations WHERE user_id = ? AND id IN `, userID, ids)
}

// Runs returns the user's runs with these IDs.
func (r ApprovalRepository) Runs(ctx context.Context, userID uint64, ids []uint64) (map[uint64]RunRef, error) {
	out := map[uint64]RunRef{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.DB.QueryContext(ctx,
		`SELECT id, trigger_summary, status FROM workflow_runs WHERE user_id = ? AND id IN (`+placeholders(len(ids))+`)`,
		idArgs(userID, ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref RunRef
		if err := rows.Scan(&ref.ID, &ref.TriggerSummary, &ref.Status); err != nil {
			return nil, err
		}
		out[ref.ID] = ref
	}
	return out, rows.Err()
}

func (r ApprovalRepository) names(ctx context.Context, query string, userID uint64, ids []uint64) (map[uint64]string, error) {
	out := map[uint64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.DB.QueryContext(ctx, query+`(`+placeholders(len(ids))+`)`, idArgs(userID, ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id   uint64
			name string
		)
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

func idArgs(userID uint64, ids []uint64) []any {
	args := make([]any, 0, len(ids)+1)
	args = append(args, userID)
	for _, id := range ids {
		args = append(args, id)
	}
	return args
}
