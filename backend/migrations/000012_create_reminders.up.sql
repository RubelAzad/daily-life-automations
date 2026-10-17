-- A reminder is standalone (title + remind_at), attached to a task (offset
-- before due_at, or a fixed remind_at), or attached to an event (offset before
-- each occurrence). remind_at is always the next time to fire; NULL means
-- inactive (e.g. the task has no upcoming due date).
CREATE TABLE IF NOT EXISTS reminders (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  task_id BIGINT UNSIGNED NULL,
  event_id BIGINT UNSIGNED NULL,
  title VARCHAR(255) NULL,
  note TEXT NULL,
  offset_minutes INT UNSIGNED NULL,
  remind_at DATETIME NULL,
  occurs_at DATETIME NULL,
  follow_up_minutes INT UNSIGNED NULL,
  max_follow_ups TINYINT UNSIGNED NOT NULL DEFAULT 0,
  follow_ups_sent TINYINT UNSIGNED NOT NULL DEFAULT 0,
  status ENUM('scheduled','sent','cancelled') NOT NULL DEFAULT 'scheduled',
  last_sent_at DATETIME NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_reminders_due (status, remind_at),
  INDEX idx_reminders_user (user_id, status, remind_at),
  INDEX idx_reminders_task (task_id),
  INDEX idx_reminders_event (event_id),
  CONSTRAINT fk_reminders_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_reminders_task FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE CASCADE,
  CONSTRAINT fk_reminders_event FOREIGN KEY (event_id) REFERENCES calendar_events (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
