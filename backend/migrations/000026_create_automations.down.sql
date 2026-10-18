ALTER TABLE ai_actions
  DROP FOREIGN KEY fk_ai_actions_automation,
  DROP INDEX idx_ai_actions_automation,
  DROP INDEX uq_ai_actions_key,
  DROP COLUMN idempotency_key,
  DROP COLUMN automation_id;
DROP TABLE IF EXISTS automations;
