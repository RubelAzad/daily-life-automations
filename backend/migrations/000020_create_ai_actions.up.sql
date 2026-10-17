-- Audit log of everything the AI assistant does: model calls (metadata
-- only, never prompts) and tool calls, including those waiting for the
-- user's approval.
CREATE TABLE IF NOT EXISTS ai_actions (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  kind VARCHAR(20) NOT NULL,              -- tool | llm_call
  source VARCHAR(20) NOT NULL,            -- chat | ping | automation | test
  tool VARCHAR(64) NULL,
  risk VARCHAR(10) NULL,                  -- read | low | high
  summary VARCHAR(500) NULL,              -- human-readable, shown when asking for approval
  input JSON NULL,                        -- redacted copy for display
  pending_input JSON NULL,                -- exact input while awaiting approval; cleared once decided
  output JSON NULL,                       -- redacted, truncated result
  status VARCHAR(20) NOT NULL,            -- succeeded | failed | pending_approval | running | rejected | expired
  error VARCHAR(1000) NULL,
  model VARCHAR(64) NULL,
  input_tokens INT UNSIGNED NULL,
  output_tokens INT UNSIGNED NULL,
  duration_ms INT UNSIGNED NULL,
  decided_at TIMESTAMP NULL,
  executed_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_ai_actions_user_created (user_id, created_at),
  INDEX idx_ai_actions_user_status (user_id, status),
  CONSTRAINT fk_ai_actions_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
