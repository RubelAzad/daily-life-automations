ALTER TABLE tasks
  ADD COLUMN project_id BIGINT UNSIGNED NULL AFTER list_id,
  ADD COLUMN milestone_id BIGINT UNSIGNED NULL AFTER project_id,
  ADD INDEX idx_tasks_user_project (user_id, project_id),
  ADD CONSTRAINT fk_tasks_project FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE SET NULL,
  ADD CONSTRAINT fk_tasks_milestone FOREIGN KEY (milestone_id) REFERENCES milestones (id) ON DELETE SET NULL;
