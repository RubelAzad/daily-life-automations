ALTER TABLE tasks
  ADD COLUMN list_id BIGINT UNSIGNED NULL AFTER user_id,
  ADD COLUMN completed_at DATETIME NULL AFTER due_at,
  ADD INDEX idx_tasks_user_list (user_id, list_id),
  ADD CONSTRAINT fk_tasks_list FOREIGN KEY (list_id) REFERENCES task_lists (id) ON DELETE SET NULL;
