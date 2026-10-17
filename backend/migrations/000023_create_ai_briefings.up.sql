-- AI daily briefings: a morning plan and an evening wrap-up. They only
-- advise; nothing is changed until the user acts on them.
CREATE TABLE IF NOT EXISTS ai_briefings (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  kind VARCHAR(10) NOT NULL,              -- morning | evening
  briefing_date DATE NOT NULL,            -- the user's local date
  trigger_source VARCHAR(10) NOT NULL,    -- manual | scheduled
  -- 1 for scheduled briefings, NULL otherwise: the unique key below then
  -- allows one scheduled briefing per user, kind and day, so concurrent
  -- workers can't generate it twice, while manual ones are unlimited.
  scheduled_slot TINYINT UNSIGNED NULL,
  status VARCHAR(12) NOT NULL,            -- generating | ready | failed
  content JSON NULL,
  error VARCHAR(1000) NULL,
  model VARCHAR(64) NULL,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_ai_briefings_scheduled (user_id, kind, briefing_date, scheduled_slot),
  INDEX idx_ai_briefings_user_date (user_id, briefing_date, kind),
  CONSTRAINT fk_ai_briefings_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Automatic briefings are off by default: each one is a paid model call.
ALTER TABLE user_settings
  ADD COLUMN morning_briefing_enabled TINYINT(1) NOT NULL DEFAULT 0 AFTER ai_approval_mode,
  ADD COLUMN morning_briefing_time CHAR(5) NOT NULL DEFAULT '07:30' AFTER morning_briefing_enabled,
  ADD COLUMN evening_summary_enabled TINYINT(1) NOT NULL DEFAULT 0 AFTER morning_briefing_time,
  ADD COLUMN evening_summary_time CHAR(5) NOT NULL DEFAULT '21:00' AFTER evening_summary_enabled;
