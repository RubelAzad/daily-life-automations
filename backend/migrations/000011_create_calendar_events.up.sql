-- Timed events store UTC instants. All-day events store UTC midnight of the
-- first day in starts_at and of the day after the last day in ends_at
-- (exclusive end); the API exposes them as start_date/end_date.
CREATE TABLE IF NOT EXISTS calendar_events (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  title VARCHAR(255) NOT NULL,
  description TEXT NULL,
  location VARCHAR(255) NULL,
  starts_at DATETIME NOT NULL,
  ends_at DATETIME NOT NULL,
  all_day BOOLEAN NOT NULL DEFAULT FALSE,
  buffer_before_minutes SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  busy BOOLEAN NOT NULL DEFAULT TRUE,
  color CHAR(7) NULL,
  recurrence ENUM('daily','weekly','monthly','yearly') NULL,
  recurrence_interval SMALLINT UNSIGNED NOT NULL DEFAULT 1,
  recurrence_until DATETIME NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_events_user_range (user_id, starts_at, ends_at),
  INDEX idx_events_user_recurring (user_id, recurrence),
  CONSTRAINT fk_events_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
