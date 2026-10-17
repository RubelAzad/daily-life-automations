package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"lifeos-ai/backend/internal/models"
)

type ProjectRepository struct {
	DB DBTX
}

type ProjectFilter struct {
	Statuses []string
	Limit    int
	Offset   int
}

// progressColumns counts the top-level tasks matching cond: total (excluding
// cancelled), completed, and overdue (open and past due).
func progressColumns(cond string) string {
	base := `SELECT COUNT(*) FROM tasks t WHERE ` + cond + ` AND t.parent_id IS NULL`
	return `(` + base + ` AND t.status <> 'cancelled'),
	        (` + base + ` AND t.status = 'completed'),
	        (` + base + ` AND t.status IN ('pending', 'in_progress') AND t.due_at < UTC_TIMESTAMP())`
}

var projectSelect = `
	SELECT p.id, p.user_id, p.name, p.description, p.status, p.color, p.start_date, p.target_date,
	       p.completed_at, p.created_at, p.updated_at,
	       ` + progressColumns("t.project_id = p.id") + `,
	       (SELECT COUNT(*) FROM milestones m WHERE m.project_id = p.id),
	       (SELECT COUNT(*) FROM milestones m WHERE m.project_id = p.id AND m.completed_at IS NOT NULL)
	  FROM projects p`

var milestoneSelect = `
	SELECT ms.id, ms.project_id, ms.title, ms.description, ms.due_date, ms.completed_at, ms.position,
	       ms.created_at, ms.updated_at,
	       ` + progressColumns("t.milestone_id = ms.id") + `
	  FROM milestones ms
	  JOIN projects p ON p.id = ms.project_id`

// List orders active work first, then by nearest target date.
func (r ProjectRepository) List(ctx context.Context, userID uint64, f ProjectFilter) ([]models.Project, int, error) {
	where := []string{"p.user_id = ?"}
	args := []any{userID}
	if len(f.Statuses) > 0 {
		where = append(where, "p.status IN ("+placeholders(len(f.Statuses))+")")
		for _, s := range f.Statuses {
			args = append(args, s)
		}
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects p WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.DB.QueryContext(ctx, projectSelect+` WHERE `+whereSQL+`
		ORDER BY FIELD(p.status, 'active', 'on_hold', 'completed', 'archived'),
		         p.target_date IS NULL, p.target_date, p.id DESC
		LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	projects := []models.Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, 0, err
		}
		projects = append(projects, p)
	}
	return projects, total, rows.Err()
}

// Get returns the project without milestones; see GetWithMilestones.
func (r ProjectRepository) Get(ctx context.Context, userID, id uint64) (models.Project, error) {
	p, err := scanProject(r.DB.QueryRowContext(ctx, projectSelect+` WHERE p.id = ? AND p.user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (r ProjectRepository) GetWithMilestones(ctx context.Context, userID, id uint64) (models.Project, error) {
	p, err := r.Get(ctx, userID, id)
	if err != nil {
		return p, err
	}
	milestones, err := r.ListMilestones(ctx, userID, id)
	p.Milestones = &milestones
	return p, err
}

func (r ProjectRepository) Create(ctx context.Context, p models.Project) (models.Project, error) {
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO projects (user_id, name, description, status, color, start_date, target_date, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.UserID, p.Name, p.Description, p.Status, p.Color, p.StartDate, p.TargetDate, utcPtr(p.CompletedAt))
	if err != nil {
		return models.Project{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Project{}, err
	}
	return r.GetWithMilestones(ctx, p.UserID, uint64(id))
}

func (r ProjectRepository) Update(ctx context.Context, p models.Project) (models.Project, error) {
	_, err := r.DB.ExecContext(ctx,
		`UPDATE projects
		    SET name = ?, description = ?, status = ?, color = ?, start_date = ?, target_date = ?, completed_at = ?
		  WHERE id = ? AND user_id = ?`,
		p.Name, p.Description, p.Status, p.Color, p.StartDate, p.TargetDate, utcPtr(p.CompletedAt), p.ID, p.UserID)
	if err != nil {
		return models.Project{}, err
	}
	return r.GetWithMilestones(ctx, p.UserID, p.ID)
}

// Delete removes the project and its milestones; tasks are kept and lose
// their project_id/milestone_id (ON DELETE SET NULL).
func (r ProjectRepository) Delete(ctx context.Context, userID, id uint64) error {
	res, err := r.DB.ExecContext(ctx, `DELETE FROM projects WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- Milestones ------------------------------------------------------------

func (r ProjectRepository) ListMilestones(ctx context.Context, userID, projectID uint64) ([]models.Milestone, error) {
	rows, err := r.DB.QueryContext(ctx,
		milestoneSelect+` WHERE ms.project_id = ? AND p.user_id = ?
		ORDER BY ms.position, ms.due_date IS NULL, ms.due_date, ms.id`, projectID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	milestones := []models.Milestone{}
	for rows.Next() {
		m, err := scanMilestone(rows)
		if err != nil {
			return nil, err
		}
		milestones = append(milestones, m)
	}
	return milestones, rows.Err()
}

// GetMilestone returns the milestone only if its project belongs to userID.
func (r ProjectRepository) GetMilestone(ctx context.Context, userID, id uint64) (models.Milestone, error) {
	m, err := scanMilestone(r.DB.QueryRowContext(ctx, milestoneSelect+` WHERE ms.id = ? AND p.user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// CreateMilestone appends after the project's existing milestones. Callers
// must have verified the project belongs to userID.
func (r ProjectRepository) CreateMilestone(ctx context.Context, userID uint64, m models.Milestone) (models.Milestone, error) {
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO milestones (project_id, title, description, due_date, completed_at, position)
		 SELECT ?, ?, ?, ?, ?, COALESCE(MAX(position), 0) + 1 FROM milestones WHERE project_id = ?`,
		m.ProjectID, m.Title, m.Description, m.DueDate, utcPtr(m.CompletedAt), m.ProjectID)
	if err != nil {
		return models.Milestone{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Milestone{}, err
	}
	return r.GetMilestone(ctx, userID, uint64(id))
}

func (r ProjectRepository) UpdateMilestone(ctx context.Context, userID uint64, m models.Milestone) (models.Milestone, error) {
	_, err := r.DB.ExecContext(ctx,
		`UPDATE milestones ms
		   JOIN projects p ON p.id = ms.project_id
		    SET ms.title = ?, ms.description = ?, ms.due_date = ?, ms.completed_at = ?, ms.position = ?
		  WHERE ms.id = ? AND p.user_id = ?`,
		m.Title, m.Description, m.DueDate, utcPtr(m.CompletedAt), m.Position, m.ID, userID)
	if err != nil {
		return models.Milestone{}, err
	}
	return r.GetMilestone(ctx, userID, m.ID)
}

// DeleteMilestone removes it; its tasks stay in the project with no milestone.
func (r ProjectRepository) DeleteMilestone(ctx context.Context, userID, id uint64) error {
	res, err := r.DB.ExecContext(ctx,
		`DELETE ms FROM milestones ms JOIN projects p ON p.id = ms.project_id WHERE ms.id = ? AND p.user_id = ?`,
		id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanProject(row rowScanner) (models.Project, error) {
	var p models.Project
	pr := &p.Progress
	err := row.Scan(&p.ID, &p.UserID, &p.Name, &p.Description, &p.Status, &p.Color, &p.StartDate, &p.TargetDate,
		&p.CompletedAt, &p.CreatedAt, &p.UpdatedAt,
		&pr.TotalTasks, &pr.CompletedTasks, &pr.OverdueTasks, &pr.Milestones, &pr.MilestonesCompleted)
	pr.ComputePercent()
	return p, err
}

func scanMilestone(row rowScanner) (models.Milestone, error) {
	var m models.Milestone
	pr := &m.Progress
	err := row.Scan(&m.ID, &m.ProjectID, &m.Title, &m.Description, &m.DueDate, &m.CompletedAt, &m.Position,
		&m.CreatedAt, &m.UpdatedAt, &pr.TotalTasks, &pr.CompletedTasks, &pr.OverdueTasks)
	m.Completed = m.CompletedAt != nil
	pr.ComputePercent()
	return m, err
}
