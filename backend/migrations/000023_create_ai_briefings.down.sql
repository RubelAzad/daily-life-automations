ALTER TABLE user_settings
  DROP COLUMN evening_summary_time,
  DROP COLUMN evening_summary_enabled,
  DROP COLUMN morning_briefing_time,
  DROP COLUMN morning_briefing_enabled;
DROP TABLE IF EXISTS ai_briefings;
