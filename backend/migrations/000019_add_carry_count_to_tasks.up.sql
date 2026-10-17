-- How many times an unfinished task was moved forward to a later day.
ALTER TABLE tasks
  ADD COLUMN carry_count INT UNSIGNED NOT NULL DEFAULT 0 AFTER due_at,
  ADD INDEX idx_tasks_user_completed (user_id, completed_at);
