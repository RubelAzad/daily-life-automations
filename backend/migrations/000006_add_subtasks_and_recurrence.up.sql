ALTER TABLE tasks
  ADD COLUMN parent_id BIGINT UNSIGNED NULL AFTER list_id,
  ADD COLUMN recurrence ENUM('daily','weekly','monthly','yearly') NULL AFTER completed_at,
  ADD COLUMN recurrence_interval SMALLINT UNSIGNED NOT NULL DEFAULT 1 AFTER recurrence,
  ADD COLUMN recurrence_until DATETIME NULL AFTER recurrence_interval,
  ADD COLUMN recurrence_anchor_at DATETIME NULL AFTER recurrence_until,
  ADD COLUMN next_task_id BIGINT UNSIGNED NULL AFTER recurrence_anchor_at,
  ADD INDEX idx_tasks_parent (parent_id),
  ADD CONSTRAINT fk_tasks_parent FOREIGN KEY (parent_id) REFERENCES tasks (id) ON DELETE CASCADE,
  ADD CONSTRAINT fk_tasks_next FOREIGN KEY (next_task_id) REFERENCES tasks (id) ON DELETE SET NULL;
