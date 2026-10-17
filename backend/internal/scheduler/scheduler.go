// Package scheduler is the durable, MySQL-backed job scheduler the worker
// runs. Jobs live in scheduled_jobs, so nothing is lost when a worker stops.
//
//   - Several workers can run at once: each job is claimed with
//     FOR UPDATE SKIP LOCKED and held with a lease that is renewed while it
//     runs. If a worker dies, its job is taken over when the lease runs out.
//   - Delivery is at least once, so handlers must be safe to repeat (use the
//     job's idempotency key for anything they create).
//   - A failed attempt is retried with exponential backoff, up to the job's
//     max attempts. Return Permanent(err) for errors a retry can't fix.
//   - Repeating jobs go back to pending after each run.
//   - Adding a job with an idempotency key that exists does nothing.
package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
)

const (
	defaultLease       = 2 * time.Minute
	defaultTimeout     = 15 * time.Minute
	defaultConcurrency = 4
	defaultMaxAttempts = 5
	retryBase          = 30 * time.Second
	retryCap           = time.Hour
)

// Job is what a handler receives.
type Job struct {
	ID      uint64
	UserID  *uint64 // nil for system jobs
	Kind    string
	Key     string // idempotency key, if any
	Attempt int    // 1 on the first try
	RunAt   time.Time
	Payload json.RawMessage
}

// Decode reads the job's payload into v.
func (j Job) Decode(v any) error {
	if len(j.Payload) == 0 {
		return Permanent(errors.New("job has no payload"))
	}
	if err := json.Unmarshal(j.Payload, v); err != nil {
		return Permanent(fmt.Errorf("bad payload: %w", err))
	}
	return nil
}

// Handler does one job. An error means the attempt failed.
type Handler func(ctx context.Context, job Job) error

type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent marks an error that retrying won't fix (bad input, missing
// data): the job fails at once instead of using up its attempts.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err}
}

// NewJob describes a job to add.
type NewJob struct {
	UserID *uint64
	Kind   string
	// Payload is stored as JSON; nil for none.
	Payload any
	// RunAt is when it is due; zero means now.
	RunAt time.Time
	// Key is the idempotency key: a second Enqueue with the same key does
	// nothing. Use something that names the work, e.g. "bill:42:2026-11".
	Key string
	// MaxAttempts defaults to 5.
	MaxAttempts int
}

// Enqueue adds a job. Pass the caller's transaction as db so the job is
// saved together with the change that needs it, or not at all. created is
// false when a job with the same key already exists; id is that job's.
func Enqueue(ctx context.Context, db repository.DBTX, j NewJob) (id uint64, created bool, err error) {
	if j.Kind == "" {
		return 0, false, errors.New("scheduler: job kind is required")
	}
	if len(j.Key) > 191 {
		return 0, false, errors.New("scheduler: idempotency key is longer than 191 bytes")
	}
	row := models.ScheduledJob{UserID: j.UserID, Kind: j.Kind, RunAt: j.RunAt, MaxAttempts: j.MaxAttempts}
	if row.RunAt.IsZero() {
		row.RunAt = time.Now()
	}
	if row.MaxAttempts <= 0 {
		row.MaxAttempts = defaultMaxAttempts
	}
	if j.Key != "" {
		row.IdempotencyKey = &j.Key
	}
	if j.Payload != nil {
		if row.Payload, err = json.Marshal(j.Payload); err != nil {
			return 0, false, err
		}
	}
	return repository.JobRepository{DB: db}.Insert(ctx, row)
}

// Cancel stops a pending job by its idempotency key. ok is false if there is
// no pending job with that key (it may have run already).
func Cancel(ctx context.Context, db repository.DBTX, key string) (bool, error) {
	return repository.JobRepository{DB: db}.CancelByKey(ctx, key)
}

// Repeating is a system job that runs every Every.
type Repeating struct {
	Kind    string
	Every   time.Duration
	Handler Handler
}

// RepeatingKey is the idempotency key of a repeating system job.
func RepeatingKey(kind string) string { return "system:" + kind }

// Scheduler claims due jobs and runs their handlers.
type Scheduler struct {
	DB       *sql.DB
	WorkerID string
	Log      *log.Logger
	// Lease is how long a claim lasts without renewal (default 2 min). It is
	// renewed every third of that while the handler runs.
	Lease time.Duration
	// Timeout bounds one attempt (default 15 min).
	Timeout time.Duration
	// Concurrency is how many jobs run at once (default 4).
	Concurrency int
	Now         func() time.Time // overridable for tests

	handlers  map[string]Handler
	repeating []Repeating
}

// New returns a scheduler with a worker ID made of the host name, the
// process ID and a timestamp.
func New(db *sql.DB, logger *log.Logger) *Scheduler {
	host, _ := os.Hostname()
	id := fmt.Sprintf("%s:%d:%d", host, os.Getpid(), time.Now().UnixNano()%1e6)
	if len(id) > 64 {
		id = id[len(id)-64:]
	}
	return &Scheduler{DB: db, WorkerID: id, Log: logger}
}

// Handle registers the handler for a job kind. Only registered kinds are
// claimed, so a worker never takes a job it can't run.
func (s *Scheduler) Handle(kind string, h Handler) {
	if s.handlers == nil {
		s.handlers = map[string]Handler{}
	}
	s.handlers[kind] = h
}

// Every registers a repeating system job; Start saves it.
func (s *Scheduler) Every(r Repeating) {
	s.Handle(r.Kind, r.Handler)
	s.repeating = append(s.repeating, r)
}

// Start saves the repeating jobs, so they exist (with their current
// intervals) before the first claim.
func (s *Scheduler) Start(ctx context.Context) error {
	jobs := repository.JobRepository{DB: s.DB}
	for _, r := range s.repeating {
		if err := jobs.EnsureRepeating(ctx, RepeatingKey(r.Kind), r.Kind, r.Every, 3, s.now()); err != nil {
			return fmt.Errorf("scheduler: register %s: %w", r.Kind, err)
		}
	}
	return nil
}

// RunNow makes every repeating job due now, for a one-off pass.
func (s *Scheduler) RunNow(ctx context.Context) error {
	for _, r := range s.repeating {
		if _, err := s.DB.ExecContext(ctx,
			`UPDATE scheduled_jobs SET run_at = ? WHERE idempotency_key = ? AND status = 'pending'`, s.now(), RepeatingKey(r.Kind)); err != nil {
			return err
		}
	}
	return nil
}

// Stats counts the attempts in a pass.
type Stats struct {
	Succeeded int
	Retrying  int // failed, will be tried again
	Failed    int // failed for good (one-off jobs)
}

// Drain runs due jobs until none are left, then returns. Repeating jobs run
// once, since each run schedules the next one in the future.
func (s *Scheduler) Drain(ctx context.Context) (Stats, error) {
	var (
		mu    sync.Mutex
		stats Stats
		wg    sync.WaitGroup
		sem   = make(chan struct{}, s.concurrency())
		seen  = map[uint64]bool{}
	)
	for ctx.Err() == nil {
		sem <- struct{}{}
		job, ok, err := s.claim(ctx)
		if err != nil || !ok || seen[job.ID] {
			<-sem
			if ok {
				// A retry already due again (backoff below the poll): put it
				// back for the next pass rather than looping on it.
				s.release(ctx, job)
			}
			wg.Wait()
			return stats, err
		}
		seen[job.ID] = true
		wg.Go(func() {
			defer func() { <-sem }()
			outcome := s.run(job)
			mu.Lock()
			stats.add(outcome)
			mu.Unlock()
		})
	}
	wg.Wait()
	return stats, ctx.Err()
}

// Loop runs jobs as they come due until ctx is cancelled, checking every
// poll. On cancel it stops claiming and waits for running jobs to finish.
func (s *Scheduler) Loop(ctx context.Context, poll time.Duration) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, s.concurrency())
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-timer.C:
		}
		// Fill the free slots, then wait for the next check.
		for len(sem) < cap(sem) && ctx.Err() == nil {
			job, ok, err := s.claim(ctx)
			if err != nil {
				s.logf("scheduler: claim failed: %v", err)
				break
			}
			if !ok {
				break
			}
			sem <- struct{}{}
			wg.Go(func() {
				defer func() { <-sem }()
				s.run(job)
			})
		}
		timer.Reset(poll)
	}
}

func (s *Scheduler) kinds() []string {
	out := make([]string, 0, len(s.handlers))
	for k := range s.handlers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *Scheduler) claim(ctx context.Context) (models.ScheduledJob, bool, error) {
	now := s.now()
	return repository.JobRepository{DB: s.DB}.ClaimNext(ctx, s.kinds(), s.WorkerID, now, now.Add(s.lease()))
}

// release hands a claimed job back untouched, apart from the attempt count.
func (s *Scheduler) release(ctx context.Context, j models.ScheduledJob) {
	_, err := repository.JobRepository{DB: s.DB}.Finish(ctx, j.ID, s.WorkerID, repository.JobOutcome{
		Status: models.JobPending, RunAt: j.RunAt, Attempts: j.Attempts - 1,
		LastStatus: deref(j.LastStatus), LastError: j.LastError, FinishedAt: s.now(),
	})
	if err != nil {
		s.logf("scheduler: release job %d: %v", j.ID, err)
	}
}

type outcome int

const (
	outcomeSucceeded outcome = iota
	outcomeRetrying
	outcomeFailed
	outcomeLost // the lease was lost; another worker owns the job now
)

func (st *Stats) add(o outcome) {
	switch o {
	case outcomeSucceeded:
		st.Succeeded++
	case outcomeRetrying:
		st.Retrying++
	case outcomeFailed:
		st.Failed++
	}
}

// run does one attempt and records how it went. It deliberately ignores the
// loop's context: on shutdown, running jobs finish rather than being cut off.
func (s *Scheduler) run(j models.ScheduledJob) outcome {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout())
	defer cancel()

	var err error
	if j.Attempts > j.MaxAttempts {
		// Claimed again after its worker died on the last allowed attempt.
		err = errors.New("the worker stopped while running this job")
	} else {
		stopRenew := s.keepLease(ctx, j.ID, cancel)
		err = s.call(ctx, j)
		stopRenew()
	}
	o, outcome := s.decide(j, err)
	ok, ferr := repository.JobRepository{DB: s.DB}.Finish(context.Background(), j.ID, s.WorkerID, o)
	switch {
	case ferr != nil:
		// The lease runs out and the job is retried.
		s.logf("scheduler: job %d (%s): could not record the result: %v", j.ID, j.Kind, ferr)
		return outcomeLost
	case !ok:
		s.logf("scheduler: job %d (%s): lease lost; another worker took it over", j.ID, j.Kind)
		return outcomeLost
	}
	if err != nil {
		s.logf("scheduler: job %d (%s) attempt %d: %v", j.ID, j.Kind, j.Attempts, err)
	}
	return outcome
}

// call runs the handler, turning a panic into an error.
func (s *Scheduler) call(ctx context.Context, j models.ScheduledJob) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("handler panicked: %v", p)
		}
	}()
	h := s.handlers[j.Kind]
	if h == nil {
		return Permanent(fmt.Errorf("no handler for %q", j.Kind))
	}
	return h(ctx, Job{ID: j.ID, UserID: j.UserID, Kind: j.Kind, Key: deref(j.IdempotencyKey), Attempt: j.Attempts, RunAt: j.RunAt, Payload: j.Payload})
}

// keepLease renews the lease while the handler runs. If the lease is lost,
// the handler's context is cancelled, since another worker may start the job.
func (s *Scheduler) keepLease(ctx context.Context, id uint64, cancel context.CancelFunc) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		t := time.NewTicker(s.lease() / 3)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				ok, err := repository.JobRepository{DB: s.DB}.Renew(ctx, id, s.WorkerID, s.now().Add(s.lease()))
				if err != nil {
					s.logf("scheduler: job %d: lease renewal failed: %v", id, err)
					continue // try again; the lease is still valid for a while
				}
				if !ok {
					cancel()
					return
				}
			}
		}
	})
	return func() { close(done); wg.Wait() }
}

// decide works out what happens to a job after an attempt.
func (s *Scheduler) decide(j models.ScheduledJob, err error) (repository.JobOutcome, outcome) {
	now := s.now()
	o := repository.JobOutcome{FinishedAt: now, Attempts: j.Attempts}
	var every time.Duration
	if j.RepeatSeconds != nil {
		every = time.Duration(*j.RepeatSeconds) * time.Second
	}

	if err == nil {
		o.LastStatus = models.JobSucceeded
		if every > 0 {
			o.Status, o.RunAt, o.Attempts = models.JobPending, nextRun(j.RunAt, every, now), 0
		} else {
			o.Status, o.RunAt = models.JobSucceeded, j.RunAt
		}
		return o, outcomeSucceeded
	}

	o.LastStatus, o.LastError = models.JobFailed, truncate(err.Error(), 1000)
	var perm permanentError
	if !errors.As(err, &perm) && j.Attempts < j.MaxAttempts {
		wait := Backoff(j.Attempts)
		if every > 0 && wait > every {
			wait = every // never later than its next regular run
		}
		o.Status, o.RunAt = models.JobPending, now.Add(wait)
		return o, outcomeRetrying
	}
	if every > 0 {
		// Out of retries: give up on this run and wait for the next.
		o.Status, o.RunAt, o.Attempts = models.JobPending, nextRun(j.RunAt, every, now), 0
		return o, outcomeFailed
	}
	o.Status, o.RunAt = models.JobFailed, j.RunAt
	return o, outcomeFailed
}

// Backoff is the wait before retry n+1 after attempt n failed: 30 s, 1 min,
// 2 min, 4 min, … up to an hour.
func Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := retryBase
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= retryCap {
			return retryCap
		}
	}
	return d
}

// nextRun keeps a repeating job on its interval, but skips runs it missed
// (e.g. while no worker was running) rather than catching up on all of them.
func nextRun(last time.Time, every time.Duration, now time.Time) time.Time {
	next := last.Add(every)
	if !next.After(now) {
		next = now.Add(every)
	}
	return next
}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Scheduler) lease() time.Duration {
	if s.Lease > 0 {
		return s.Lease
	}
	return defaultLease
}

func (s *Scheduler) timeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return defaultTimeout
}

func (s *Scheduler) concurrency() int {
	if s.Concurrency > 0 {
		return s.Concurrency
	}
	return defaultConcurrency
}

func (s *Scheduler) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

func truncate(s string, n int) *string {
	if utf8.RuneCountInString(s) > n {
		s = string([]rune(s)[:n-1]) + "…"
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
