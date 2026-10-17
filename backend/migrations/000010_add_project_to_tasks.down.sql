ALTER TABLE tasks
  DROP FOREIGN KEY fk_tasks_milestone,
  DROP FOREIGN KEY fk_tasks_project,
  DROP INDEX idx_tasks_user_project,
  DROP COLUMN milestone_id,
  DROP COLUMN project_id;
