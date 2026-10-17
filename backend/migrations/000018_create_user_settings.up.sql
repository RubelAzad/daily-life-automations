-- One row per user. Missing rows read as the defaults, so older accounts
-- keep working; the INSERT below fills them in for accounts that exist now.
CREATE TABLE IF NOT EXISTS user_settings (
  user_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
  -- IANA time zone name, e.g. "Asia/Dhaka". Used for repeating dates,
  -- "today", expiry reminders and quiet hours.
  timezone VARCHAR(64) NOT NULL DEFAULT 'UTC',
  -- 0 = Sunday, 1 = Monday, 6 = Saturday.
  week_starts_on TINYINT UNSIGNED NOT NULL DEFAULT 1,
  quiet_hours_enabled TINYINT(1) NOT NULL DEFAULT 0,
  -- "HH:MM" local time; start after end means the window crosses midnight.
  quiet_hours_start CHAR(5) NOT NULL DEFAULT '22:00',
  quiet_hours_end CHAR(5) NOT NULL DEFAULT '07:00',
  -- What the AI assistant may do without asking. High-impact actions
  -- (sending, spending, deleting, booking) always need approval.
  ai_approval_mode VARCHAR(20) NOT NULL DEFAULT 'always',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  CONSTRAINT fk_user_settings_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT chk_user_settings_week CHECK (week_starts_on IN (0, 1, 6)),
  CONSTRAINT chk_user_settings_approval CHECK (ai_approval_mode IN ('always', 'high_impact_only'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT IGNORE INTO user_settings (user_id) SELECT id FROM users;
