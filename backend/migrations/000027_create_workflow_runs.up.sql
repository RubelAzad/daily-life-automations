-- One row per automation run: what triggered it, each step's outcome, and
-- retries. Runs stay when their automation is deleted (automation_id goes
-- NULL; automation_name keeps the name).
CREATE TABLE IF NOT EXISTS workflow_runs (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  automation_id BIGINT UNSIGNED NULL,
  automation_name VARCHAR(100) NOT NULL,
  trigger_type VARCHAR(40) NOT NULL,
  trigger_source VARCHAR(10) NOT NULL,    -- schedule | event | manual
  trigger_summary VARCHAR(255) NULL,      -- e.g. the task's title
  trigger_data JSON NULL,                 -- the event's snapshot, for retries
  depth TINYINT UNSIGNED NOT NULL DEFAULT 0,
  -- Names the occurrence (one schedule time, one event, one Run press), so
  -- a run job that is retried after a crash resumes the same run.
  run_key VARCHAR(191) NOT NULL,
  status VARCHAR(20) NOT NULL,            -- running | succeeded | waiting_approval | retrying | failed | skipped
  error VARCHAR(1000) NULL,
  steps JSON NOT NULL,                    -- [{index, type, status, action_id, summary, error, attempt}]
  attempt INT UNSIGNED NOT NULL DEFAULT 1,
  next_retry_at TIMESTAMP(3) NULL,
  started_at TIMESTAMP(3) NOT NULL,
  finished_at TIMESTAMP(3) NULL,
  duration_ms INT UNSIGNED NULL,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_workflow_runs_key (run_key),
  INDEX idx_workflow_runs_user (user_id, id),
  INDEX idx_workflow_runs_automation (automation_id, id),
  INDEX idx_workflow_runs_status (user_id, status),
  CONSTRAINT fk_workflow_runs_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_workflow_runs_automation FOREIGN KEY (automation_id) REFERENCES automations (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
