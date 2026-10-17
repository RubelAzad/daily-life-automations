//go:build integration

// Runs the scheduler against the real database from backend/.env:
//
//	go test -tags integration ./internal/scheduler/
//
// Each test uses its own job kinds, so it ignores other jobs in the table,
// and deletes its jobs again.
package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lifeos-ai/backend/internal/config"
	"lifeos-ai/backend/internal/database"
	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	if err := os.Chdir("../.."); err != nil { // backend/, where .env lives
		panic(err)
	}
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	if testDB, err = database.Open(cfg.DB.DSN()); err != nil {
		panic(err)
	}
	code := m.Run()
	testDB.Close()
	os.Exit(code)
}

// clock is a settable time shared by a test's schedulers.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }
func newClock() *clock               { return &clock{t: time.Now().UTC().Truncate(time.Millisecond)} }
func uniqueKind(t *testing.T) string {
	return fmt.Sprintf("test.%.24s.%d", t.Name()[4:], time.Now().UnixNano())
}
func uniqueKey(t *testing.T, s string) string {
	return fmt.Sprintf("test:%d:%s", time.Now().UnixNano(), s)
}

func newScheduler(t *testing.T, c *clock, worker string) *Scheduler {
	return &Scheduler{DB: testDB, WorkerID: worker, Now: c.Now, Lease: time.Minute}
}

func cleanup(t *testing.T, kinds ...string) {
	t.Cleanup(func() {
		for _, k := range kinds {
			_, _ = testDB.Exec(`DELETE FROM scheduled_jobs WHERE kind = ?`, k)
		}
	})
}

func getJob(t *testing.T, id uint64) models.ScheduledJob {
	t.Helper()
	j, err := repository.JobRepository{DB: testDB}.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func enqueue(t *testing.T, j NewJob) uint64 {
	t.Helper()
	id, created, err := Enqueue(context.Background(), testDB, j)
	if err != nil || !created {
		t.Fatalf("enqueue: created=%v err=%v", created, err)
	}
	return id
}

func TestEnqueueIsIdempotent(t *testing.T) {
	ctx, kind := context.Background(), uniqueKind(t)
	cleanup(t, kind)
	key := uniqueKey(t, "once")
	id1, created1, err := Enqueue(ctx, testDB, NewJob{Kind: kind, Key: key, Payload: map[string]int{"n": 1}})
	if err != nil || !created1 {
		t.Fatal(created1, err)
	}
	id2, created2, err := Enqueue(ctx, testDB, NewJob{Kind: kind, Key: key, Payload: map[string]int{"n": 2}})
	if err != nil || created2 || id2 != id1 {
		t.Fatalf("second enqueue: id=%d (want %d) created=%v err=%v", id2, id1, created2, err)
	}
	if string(getJob(t, id1).Payload) != `{"n": 1}` {
		t.Errorf("payload changed: %s", getJob(t, id1).Payload)
	}

	// Jobs without a key never clash.
	enqueue(t, NewJob{Kind: kind})
	enqueue(t, NewJob{Kind: kind})

	// Enqueued in a transaction that rolls back: no job.
	rolledBack := uniqueKey(t, "rollback")
	tx, _ := testDB.Begin()
	if _, _, err := Enqueue(ctx, tx, NewJob{Kind: kind, Key: rolledBack}); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	if _, created, _ := Enqueue(ctx, testDB, NewJob{Kind: kind, Key: rolledBack}); !created {
		t.Error("a job from a rolled-back transaction must not exist")
	}
}

func TestRunRetryAndSucceed(t *testing.T) {
	ctx, kind, c := context.Background(), uniqueKind(t), newClock()
	cleanup(t, kind)
	s := newScheduler(t, c, "w1")
	var calls []int
	s.Handle(kind, func(_ context.Context, j Job) error {
		var p struct{ Bill int }
		if err := j.Decode(&p); err != nil {
			return err
		}
		calls = append(calls, j.Attempt)
		if j.Attempt == 1 {
			return errors.New("temporary outage")
		}
		if p.Bill != 42 || j.Key == "" {
			return Permanent(fmt.Errorf("bad job %+v", j))
		}
		return nil
	})
	id := enqueue(t, NewJob{Kind: kind, Key: uniqueKey(t, "bill"), Payload: map[string]int{"Bill": 42}, RunAt: c.Now()})

	st, err := s.Drain(ctx)
	if err != nil || st.Retrying != 1 {
		t.Fatalf("first pass: %+v %v", st, err)
	}
	j := getJob(t, id)
	if j.Status != models.JobPending || j.Attempts != 1 || j.LastError == nil || *j.LastError != "temporary outage" || !j.RunAt.Equal(c.Now().Add(30*time.Second)) {
		t.Fatalf("after failure: %+v", j)
	}
	// Not due yet.
	if st, _ := s.Drain(ctx); st != (Stats{}) {
		t.Fatalf("ran before the backoff: %+v", st)
	}
	c.Add(31 * time.Second)
	if st, _ := s.Drain(ctx); st.Succeeded != 1 {
		t.Fatalf("retry: %+v", st)
	}
	j = getJob(t, id)
	if j.Status != models.JobSucceeded || j.Attempts != 2 || j.LockedBy != nil || len(calls) != 2 {
		t.Errorf("after retry: %+v calls=%v", j, calls)
	}
}

func TestFailures(t *testing.T) {
	ctx, kind, c := context.Background(), uniqueKind(t), newClock()
	cleanup(t, kind)
	s := newScheduler(t, c, "w1")
	s.Handle(kind, func(_ context.Context, j Job) error {
		switch string(j.Payload) {
		case `"permanent"`:
			return Permanent(errors.New("no such bill"))
		case `"panic"`:
			panic("kaboom")
		}
		return errors.New("still down")
	})
	perm := enqueue(t, NewJob{Kind: kind, Payload: "permanent", RunAt: c.Now()})
	limited := enqueue(t, NewJob{Kind: kind, Payload: "flaky", RunAt: c.Now(), MaxAttempts: 2})
	panics := enqueue(t, NewJob{Kind: kind, Payload: "panic", RunAt: c.Now(), MaxAttempts: 1})

	_, _ = s.Drain(ctx)
	if j := getJob(t, perm); j.Status != models.JobFailed || j.Attempts != 1 {
		t.Errorf("permanent: %+v", j)
	}
	if j := getJob(t, panics); j.Status != models.JobFailed || j.LastError == nil || *j.LastError != "handler panicked: kaboom" {
		t.Errorf("panic: %+v", j)
	}
	if j := getJob(t, limited); j.Status != models.JobPending {
		t.Errorf("first failure should retry: %+v", j)
	}
	c.Add(time.Minute)
	_, _ = s.Drain(ctx)
	if j := getJob(t, limited); j.Status != models.JobFailed || j.Attempts != 2 {
		t.Errorf("out of attempts: %+v", j)
	}
}

func TestEachJobRunsOnceAcrossWorkers(t *testing.T) {
	ctx, kind, c := context.Background(), uniqueKind(t), newClock()
	cleanup(t, kind)
	var mu sync.Mutex
	runs := map[uint64]int{}
	handler := func(_ context.Context, j Job) error {
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		runs[j.ID]++
		mu.Unlock()
		return nil
	}
	const n = 40
	for i := 0; i < n; i++ {
		enqueue(t, NewJob{Kind: kind, RunAt: c.Now()})
	}
	var wg sync.WaitGroup
	var total atomic.Int64
	for w := 0; w < 3; w++ {
		s := newScheduler(t, c, fmt.Sprintf("w%d", w))
		s.Handle(kind, handler)
		wg.Go(func() {
			st, err := s.Drain(ctx)
			if err != nil {
				t.Error(err)
			}
			total.Add(int64(st.Succeeded))
		})
	}
	wg.Wait()
	if len(runs) != n || total.Load() != n {
		t.Fatalf("ran %d distinct jobs, %d successes; want %d", len(runs), total.Load(), n)
	}
	for id, k := range runs {
		if k != 1 {
			t.Errorf("job %d ran %d times", id, k)
		}
	}
}

func TestDeadWorkerJobIsTakenOver(t *testing.T) {
	ctx, kind, c := context.Background(), uniqueKind(t), newClock()
	cleanup(t, kind)
	id := enqueue(t, NewJob{Kind: kind, RunAt: c.Now(), MaxAttempts: 3})

	// Worker "dead" claims the job and never finishes it.
	jobs := repository.JobRepository{DB: testDB}
	claimed, ok, err := jobs.ClaimNext(ctx, []string{kind}, "dead", c.Now(), c.Now().Add(time.Minute))
	if err != nil || !ok || claimed.ID != id {
		t.Fatal(ok, err)
	}

	alive := newScheduler(t, c, "alive")
	var ran atomic.Int32
	alive.Handle(kind, func(context.Context, Job) error { ran.Add(1); return nil })
	if st, _ := alive.Drain(ctx); st != (Stats{}) || ran.Load() != 0 {
		t.Fatalf("took a job with a live lease: %+v", st)
	}
	c.Add(61 * time.Second)
	if st, _ := alive.Drain(ctx); st.Succeeded != 1 || ran.Load() != 1 {
		t.Fatalf("expired lease not taken over: %+v", st)
	}
	if j := getJob(t, id); j.Status != models.JobSucceeded || j.Attempts != 2 {
		t.Errorf("after takeover: %+v", j)
	}

	// The dead worker coming back can't overwrite the result.
	if ok, _ := jobs.Finish(ctx, id, "dead", repository.JobOutcome{Status: models.JobFailed, RunAt: c.Now(), LastStatus: models.JobFailed, FinishedAt: c.Now()}); ok {
		t.Error("a worker without the lease must not record a result")
	}
	if j := getJob(t, id); j.Status != models.JobSucceeded {
		t.Errorf("result overwritten: %+v", j)
	}
}

func TestDeadOnLastAttemptFails(t *testing.T) {
	ctx, kind, c := context.Background(), uniqueKind(t), newClock()
	cleanup(t, kind)
	id := enqueue(t, NewJob{Kind: kind, RunAt: c.Now(), MaxAttempts: 1})
	if _, ok, _ := (repository.JobRepository{DB: testDB}).ClaimNext(ctx, []string{kind}, "dead", c.Now(), c.Now().Add(time.Minute)); !ok {
		t.Fatal("claim")
	}
	c.Add(2 * time.Minute)
	s := newScheduler(t, c, "alive")
	s.Handle(kind, func(context.Context, Job) error { t.Error("must not run past max attempts"); return nil })
	_, _ = s.Drain(ctx)
	if j := getJob(t, id); j.Status != models.JobFailed || j.LastError == nil {
		t.Errorf("got %+v", j)
	}
}

func TestLeaseRenewedWhileRunning(t *testing.T) {
	ctx, kind := context.Background(), uniqueKind(t)
	cleanup(t, kind)
	s := &Scheduler{DB: testDB, WorkerID: "slow", Lease: 300 * time.Millisecond}
	id := enqueue(t, NewJob{Kind: kind})
	var leaseSeen time.Time
	s.Handle(kind, func(context.Context, Job) error {
		time.Sleep(700 * time.Millisecond) // longer than the lease
		leaseSeen = *getJob(t, id).LockedUntil
		return nil
	})
	other := &Scheduler{DB: testDB, WorkerID: "other"}
	other.Handle(kind, func(context.Context, Job) error { t.Error("job taken over while its worker was alive"); return nil })

	done := make(chan Stats)
	go func() { st, _ := s.Drain(ctx); done <- st }()
	time.Sleep(450 * time.Millisecond)
	_, _ = other.Drain(ctx)
	if st := <-done; st.Succeeded != 1 {
		t.Fatalf("slow job: %+v", st)
	}
	if !leaseSeen.After(time.Now().Add(-300 * time.Millisecond)) {
		t.Errorf("lease was not renewed: %s", leaseSeen)
	}
}

func TestOnlyRegisteredKindsAreClaimed(t *testing.T) {
	ctx, kind, other, c := context.Background(), uniqueKind(t), uniqueKind(t)+".unknown", newClock()
	cleanup(t, kind, other)
	id := enqueue(t, NewJob{Kind: other, RunAt: c.Now()})
	s := newScheduler(t, c, "w1")
	s.Handle(kind, func(context.Context, Job) error { return nil })
	_, _ = s.Drain(ctx)
	if j := getJob(t, id); j.Status != models.JobPending || j.Attempts != 0 {
		t.Errorf("claimed a job it has no handler for: %+v", j)
	}
}

func TestCancel(t *testing.T) {
	ctx, kind, c := context.Background(), uniqueKind(t), newClock()
	cleanup(t, kind)
	key := uniqueKey(t, "cancel")
	id := enqueue(t, NewJob{Kind: kind, Key: key, RunAt: c.Now()})
	if ok, err := Cancel(ctx, testDB, key); !ok || err != nil {
		t.Fatal(ok, err)
	}
	s := newScheduler(t, c, "w1")
	s.Handle(kind, func(context.Context, Job) error { t.Error("cancelled job ran"); return nil })
	_, _ = s.Drain(ctx)
	if j := getJob(t, id); j.Status != models.JobCancelled {
		t.Errorf("got %+v", j)
	}
	if ok, _ := Cancel(ctx, testDB, key); ok {
		t.Error("cancelling twice must report false")
	}
}

func TestRepeatingJobs(t *testing.T) {
	ctx, kind, c := context.Background(), uniqueKind(t), newClock()
	cleanup(t, kind)
	var runs atomic.Int32
	register := func(every time.Duration) *Scheduler {
		s := newScheduler(t, c, "w1")
		s.Every(Repeating{Kind: kind, Every: every, Handler: func(context.Context, Job) error { runs.Add(1); return nil }})
		if err := s.Start(ctx); err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := register(10 * time.Minute)
	// Registering again (a second worker, or a restart) keeps one job.
	register(10 * time.Minute)
	var count int
	_ = testDB.QueryRow(`SELECT COUNT(*) FROM scheduled_jobs WHERE kind = ?`, kind).Scan(&count)
	if count != 1 {
		t.Fatalf("%d rows for one repeating job", count)
	}

	_, _ = s.Drain(ctx)
	if runs.Load() != 1 {
		t.Fatalf("first run: %d", runs.Load())
	}
	j := getJob(t, mustID(t, kind))
	if j.Status != models.JobPending || j.Attempts != 0 || !j.RunAt.Equal(c.Now().Add(10*time.Minute)) || j.LastStatus == nil || *j.LastStatus != models.JobSucceeded {
		t.Fatalf("after run: %+v", j)
	}
	_, _ = s.Drain(ctx)
	if runs.Load() != 1 {
		t.Fatal("ran again before its interval")
	}

	// A shorter interval brings the next run forward.
	s = register(time.Minute)
	c.Add(61 * time.Second)
	_, _ = s.Drain(ctx)
	if runs.Load() != 2 {
		t.Fatalf("after shortening the interval: %d runs", runs.Load())
	}

	// RunNow makes it due at once.
	if err := s.RunNow(ctx); err != nil {
		t.Fatal(err)
	}
	_, _ = s.Drain(ctx)
	if runs.Load() != 3 {
		t.Fatalf("after RunNow: %d runs", runs.Load())
	}

	st, err := Status(ctx, testDB, c.Now())
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, js := range st.Jobs {
		if js.Kind == kind {
			found = js.EverySeconds == 60 && js.NextRunAt != nil && js.LastStatus != nil
		}
	}
	if !found || !st.Running {
		t.Errorf("status: %+v", st)
	}
}

func mustID(t *testing.T, kind string) uint64 {
	t.Helper()
	var id uint64
	if err := testDB.QueryRow(`SELECT id FROM scheduled_jobs WHERE kind = ?`, kind).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
