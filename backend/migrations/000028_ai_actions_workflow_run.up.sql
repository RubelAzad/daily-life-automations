-- Links an automation's actions to the run that took them, so the approvals
-- inbox can say which run is waiting and a decision updates that run at
-- once. The status index serves the sweep that expires old requests.
ALTER TABLE ai_actions
  ADD COLUMN workflow_run_id BIGINT UNSIGNED NULL AFTER automation_id,
  ADD INDEX idx_ai_actions_run (workflow_run_id),
  ADD INDEX idx_ai_actions_status_created (status, created_at),
  ADD CONSTRAINT fk_ai_actions_run FOREIGN KEY (workflow_run_id) REFERENCES workflow_runs (id) ON DELETE SET NULL;

-- Existing runs already list their actions in steps.
UPDATE ai_actions a
  JOIN (
    SELECT r.id AS run_id, s.action_id
      FROM workflow_runs r,
           JSON_TABLE(r.steps, '$[*]' COLUMNS (action_id BIGINT UNSIGNED PATH '$.action_id')) s
     WHERE s.action_id IS NOT NULL
  ) x ON x.action_id = a.id
   SET a.workflow_run_id = x.run_id;
