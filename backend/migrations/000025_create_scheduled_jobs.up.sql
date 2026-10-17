-- Durable background jobs, run by the worker. A job is claimed with a lease
-- (locked_by, locked_until), so it survives restarts: if a worker dies
-- mid-job, another one takes it over once the lease runs out. Delivery is at
-- least once, so handlers must be safe to repeat.
CREATE TABLE IF NOT EXISTS scheduled_jobs (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NULL,           -- NULL: a system job
  kind VARCHAR(64) NOT NULL,              -- selects the handler, e.g. reminders.deliver
  payload JSON NULL,
  -- Adding a job with a key that already exists does nothing, so the same
  -- work can't be scheduled twice. NULL keys never clash.
  idempotency_key VARCHAR(191) NULL,
  status VARCHAR(12) NOT NULL DEFAULT 'pending', -- pending | running | succeeded | failed | cancelled
  run_at TIMESTAMP(3) NOT NULL,           -- due time (UTC); after a failure, the next retry
  repeat_seconds INT UNSIGNED NULL,       -- set for repeating jobs, which go back to pending after each run
  attempts INT UNSIGNED NOT NULL DEFAULT 0,      -- in the current run; reset when a repeating job completes
  max_attempts INT UNSIGNED NOT NULL DEFAULT 5,
  locked_by VARCHAR(64) NULL,             -- worker holding the lease
  locked_until TIMESTAMP(3) NULL,
  last_status VARCHAR(12) NULL,           -- outcome of the latest attempt: succeeded | failed
  last_error VARCHAR(1000) NULL,
  last_started_at TIMESTAMP(3) NULL,
  last_finished_at TIMESTAMP(3) NULL,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_scheduled_jobs_key (idempotency_key),
  INDEX idx_scheduled_jobs_due (status, run_at),
  INDEX idx_scheduled_jobs_lease (status, locked_until),
  INDEX idx_scheduled_jobs_user (user_id, created_at),
  CONSTRAINT fk_scheduled_jobs_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
