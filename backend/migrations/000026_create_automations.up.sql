-- Automations: when a trigger fires (a schedule, an event such as
-- task.created, or the user pressing Run), and the conditions hold, the
-- actions run through the same allowlisted tools, approval policy and audit
-- log as the AI assistant.
CREATE TABLE IF NOT EXISTS automations (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(100) NOT NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  trigger_type VARCHAR(40) NOT NULL,      -- schedule | manual | task.created | task.completed | task.overdue | note.created
  trigger_config JSON NULL,               -- the schedule, for schedule triggers
  conditions JSON NOT NULL,               -- [{field, op, value}], all must hold
  actions JSON NOT NULL,                  -- [{type, input}], run in order
  next_run_at TIMESTAMP(3) NULL,          -- schedule triggers: the next run (UTC)
  last_run_at TIMESTAMP(3) NULL,
  last_status VARCHAR(20) NULL,           -- succeeded | waiting_approval | failed | skipped
  last_error VARCHAR(1000) NULL,
  run_count INT UNSIGNED NOT NULL DEFAULT 0,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  INDEX idx_automations_trigger (user_id, enabled, trigger_type),
  CONSTRAINT fk_automations_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Actions taken by an automation point back to it. The idempotency key
-- makes a retried run skip the actions it already took.
ALTER TABLE ai_actions
  ADD COLUMN automation_id BIGINT UNSIGNED NULL AFTER conversation_id,
  ADD COLUMN idempotency_key VARCHAR(191) NULL AFTER automation_id,
  ADD UNIQUE KEY uq_ai_actions_key (idempotency_key),
  ADD INDEX idx_ai_actions_automation (automation_id, created_at),
  ADD CONSTRAINT fk_ai_actions_automation FOREIGN KEY (automation_id) REFERENCES automations (id) ON DELETE SET NULL;
