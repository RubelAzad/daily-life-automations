ALTER TABLE tasks
  DROP INDEX idx_tasks_user_completed,
  DROP COLUMN carry_count;
