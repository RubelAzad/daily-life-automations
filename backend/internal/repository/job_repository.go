package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"lifeos-ai/backend/internal/models"
)

// JobRepository stores the durable scheduler's jobs. Times are UTC and come
// from the caller, never NOW(), so tests can control the clock.
type JobRepository struct {
	DB DBTX
}

const jobColumns = `id, user_id, kind, payload, idempotency_key, status, run_at, repeat_seconds, attempts, max_attempts,
	locked_by, locked_until, last_status, last_error, last_started_at, last_finished_at, created_at`

// Insert adds a pending job. When its idempotency key already exists,
// nothing changes and created is false; id is the existing job's either way.
func (r JobRepository) Insert(ctx context.Context, j models.ScheduledJob) (id uint64, created bool, err error) {
	var payload any
	if len(j.Payload) > 0 {
		payload = string(j.Payload)
	}
	// LAST_INSERT_ID(id) makes a duplicate report the existing row's id, with
	// 0 rows affected (the update changes nothing).
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO scheduled_jobs (user_id, kind, payload, idempotency_key, status, run_at, repeat_seconds, max_attempts)
		 VALUES (?, ?, ?, ?, 'pending', ?, ?, ?)
		 ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		j.UserID, j.Kind, payload, j.IdempotencyKey, j.RunAt.UTC(), j.RepeatSeconds, j.MaxAttempts)
	if err != nil {
		return 0, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, false, err
	}
	last, err := res.LastInsertId()
	return uint64(last), n == 1, err
}

// EnsureRepeating registers a repeating system job under key, or updates its
// kind and interval. A job that isn't running is brought forward to run
// within one interval, and a stopped one is started again.
func (r JobRepository) EnsureRepeating(ctx context.Context, key, kind string, every time.Duration, maxAttempts int, now time.Time) error {
	secs, now := int(every/time.Second), now.UTC()
	next := now.Add(every)
	// Assignments run left to right, so run_at still sees the old status.
	_, err := r.DB.ExecContext(ctx,
		`INSERT INTO scheduled_jobs (kind, idempotency_key, status, run_at, repeat_seconds, max_attempts)
		 VALUES (?, ?, 'pending', ?, ?, ?)
		 ON DUPLICATE KEY UPDATE
		   kind = ?, repeat_seconds = ?, max_attempts = ?,
		   run_at = IF(status = 'running', run_at, IF(status = 'pending', LEAST(run_at, ?), ?)),
		   status = IF(status = 'running', status, 'pending')`,
		kind, key, now, secs, maxAttempts, kind, secs, maxAttempts, next, now)
	return err
}

// ClaimNext locks the next due job of one of kinds for worker until
// leaseUntil and counts the attempt. A running job whose lease ran out (its
// worker died) counts as due. ok is false when nothing is due.
func (r JobRepository) ClaimNext(ctx context.Context, kinds []string, worker string, now, leaseUntil time.Time) (job models.ScheduledJob, ok bool, err error) {
	if len(kinds) == 0 {
		return job, false, nil
	}
	args := make([]any, 0, len(kinds)+2)
	for _, k := range kinds {
		args = append(args, k)
	}
	args = append(args, now.UTC(), now.UTC())
	// READ COMMITTED: claiming must not gap-lock the table, or it deadlocks
	// with transactions adding jobs (a task being created queues its event).
	err = retry(func() error {
		job, ok = models.ScheduledJob{}, false
		return inTxReadCommitted(ctx, r.DB, func(tx DBTX) error {
			// SKIP LOCKED: workers claiming at the same time take different jobs
			// instead of waiting for each other.
			row := tx.QueryRowContext(ctx,
				`SELECT `+jobColumns+` FROM scheduled_jobs
			  WHERE kind IN (?`+strings.Repeat(", ?", len(kinds)-1)+`)
			    AND ((status = 'pending' AND run_at <= ?) OR (status = 'running' AND locked_until < ?))
			  ORDER BY run_at, id
			  LIMIT 1
			  FOR UPDATE SKIP LOCKED`, args...)
			j, err := scanJob(row)
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx,
				`UPDATE scheduled_jobs SET status = 'running', attempts = attempts + 1, locked_by = ?, locked_until = ?, last_started_at = ? WHERE id = ?`,
				worker, leaseUntil.UTC(), now.UTC(), j.ID); err != nil {
				return err
			}
			j.Status, j.Attempts, j.LockedBy, j.LastStartedAt = models.JobRunning, j.Attempts+1, &worker, &now
			lu := leaseUntil.UTC()
			j.LockedUntil = &lu
			job, ok = j, true
			return nil
		})
	})
	return job, ok, err
}

// CountDue counts due jobs of the kinds without locking anything. A claim
// can find nothing while due jobs exist, if another claimer is looking at
// them at that moment (SKIP LOCKED skips them); this tells the two apart.
func (r JobRepository) CountDue(ctx context.Context, kinds []string, now time.Time) (int, error) {
	if len(kinds) == 0 {
		return 0, nil
	}
	args := make([]any, 0, len(kinds)+2)
	for _, k := range kinds {
		args = append(args, k)
	}
	args = append(args, now.UTC(), now.UTC())
	var n int
	err := r.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM scheduled_jobs
		  WHERE kind IN (?`+strings.Repeat(", ?", len(kinds)-1)+`)
		    AND ((status = 'pending' AND run_at <= ?) OR (status = 'running' AND locked_until < ?))`, args...).Scan(&n)
	return n, err
}

// Renew extends a lease the worker still holds. ok is false if it lost it.
func (r JobRepository) Renew(ctx context.Context, id uint64, worker string, until time.Time) (bool, error) {
	res, err := r.DB.ExecContext(ctx,
		`UPDATE scheduled_jobs SET locked_until = ? WHERE id = ? AND locked_by = ? AND status = 'running'`, until.UTC(), id, worker)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// JobOutcome is how an attempt ended and what happens to the job next.
type JobOutcome struct {
	Status     string    // the job's new status
	RunAt      time.Time // next run or retry, for a job going back to pending
	Attempts   int       // attempts to keep (0 after a repeating job completes)
	LastStatus string    // succeeded | failed; "" keeps none
	LastError  *string
	FinishedAt time.Time
}

// Finish records an attempt and releases the lease. Only the worker holding
// the lease can do this, so a worker whose lease ran out (and whose job was
// taken over) can't overwrite the newer attempt. ok is false in that case.
func (r JobRepository) Finish(ctx context.Context, id uint64, worker string, o JobOutcome) (ok bool, err error) {
	err = retry(func() error {
		ok, err = r.finish(ctx, id, worker, o)
		return err
	})
	return ok, err
}

func (r JobRepository) finish(ctx context.Context, id uint64, worker string, o JobOutcome) (bool, error) {
	res, err := r.DB.ExecContext(ctx,
		`UPDATE scheduled_jobs
		    SET status = ?, run_at = ?, attempts = ?, last_status = NULLIF(?, ''), last_error = ?, last_finished_at = ?,
		        locked_by = NULL, locked_until = NULL
		  WHERE id = ? AND locked_by = ? AND status = 'running'`,
		o.Status, o.RunAt.UTC(), o.Attempts, o.LastStatus, o.LastError, o.FinishedAt.UTC(), id, worker)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// CancelByKey stops a job that hasn't started yet. ok is false if there is
// no such pending job.
func (r JobRepository) CancelByKey(ctx context.Context, key string) (bool, error) {
	res, err := r.DB.ExecContext(ctx, `UPDATE scheduled_jobs SET status = 'cancelled' WHERE idempotency_key = ? AND status = 'pending'`, key)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (r JobRepository) Get(ctx context.Context, id uint64) (models.ScheduledJob, error) {
	j, err := scanJob(r.DB.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM scheduled_jobs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return j, ErrNotFound
	}
	return j, err
}

// Repeating returns the repeating system jobs, by kind.
func (r JobRepository) Repeating(ctx context.Context) ([]models.ScheduledJob, error) {
	rows, err := r.DB.QueryContext(ctx,
		`SELECT `+jobColumns+` FROM scheduled_jobs WHERE user_id IS NULL AND repeat_seconds IS NOT NULL AND status <> 'cancelled' ORDER BY kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ScheduledJob
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// PruneFinished deletes one-off jobs that ended before the cutoffs, up to
// limit rows. Their idempotency keys can then be used again.
func (r JobRepository) PruneFinished(ctx context.Context, doneBefore, failedBefore time.Time, limit int) (int64, error) {
	res, err := r.DB.ExecContext(ctx,
		`DELETE FROM scheduled_jobs
		  WHERE repeat_seconds IS NULL
		    AND ((status IN ('succeeded', 'cancelled') AND updated_at < ?) OR (status = 'failed' AND updated_at < ?))
		  LIMIT ?`, doneBefore.UTC(), failedBefore.UTC(), limit)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func scanJob(row rowScanner) (models.ScheduledJob, error) {
	var j models.ScheduledJob
	var payload []byte
	err := row.Scan(&j.ID, &j.UserID, &j.Kind, &payload, &j.IdempotencyKey, &j.Status, &j.RunAt, &j.RepeatSeconds, &j.Attempts, &j.MaxAttempts,
		&j.LockedBy, &j.LockedUntil, &j.LastStatus, &j.LastError, &j.LastStartedAt, &j.LastFinishedAt, &j.CreatedAt)
	if len(payload) > 0 {
		j.Payload = payload
	}
	return j, err
}
