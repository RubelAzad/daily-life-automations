ALTER TABLE tasks
  DROP FOREIGN KEY fk_tasks_list,
  DROP INDEX idx_tasks_user_list,
  DROP COLUMN completed_at,
  DROP COLUMN list_id;
