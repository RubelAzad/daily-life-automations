ALTER TABLE tasks
  DROP FOREIGN KEY fk_tasks_next,
  DROP FOREIGN KEY fk_tasks_parent,
  DROP INDEX idx_tasks_parent,
  DROP COLUMN next_task_id,
  DROP COLUMN recurrence_anchor_at,
  DROP COLUMN recurrence_until,
  DROP COLUMN recurrence_interval,
  DROP COLUMN recurrence,
  DROP COLUMN parent_id;
