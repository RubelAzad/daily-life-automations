ALTER TABLE ai_actions
  DROP FOREIGN KEY fk_ai_actions_run,
  DROP INDEX idx_ai_actions_run,
  DROP INDEX idx_ai_actions_status_created,
  DROP COLUMN workflow_run_id;
