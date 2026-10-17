-- In-app notifications. idempotency_key makes delivery safe to retry: the
-- worker inserts with INSERT IGNORE, so a crash or a second worker can never
-- create the same notification twice.
CREATE TABLE IF NOT EXISTS notifications (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  kind VARCHAR(40) NOT NULL,
  title VARCHAR(255) NOT NULL,
  body VARCHAR(1000) NULL,
  link VARCHAR(255) NULL,
  priority ENUM('low','normal','high') NOT NULL DEFAULT 'normal',
  reminder_id BIGINT UNSIGNED NULL,
  occurs_at DATETIME NULL,
  idempotency_key VARCHAR(100) NOT NULL,
  read_at DATETIME NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uq_notifications_idempotency (idempotency_key),
  INDEX idx_notifications_user (user_id, read_at, created_at),
  CONSTRAINT fk_notifications_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_notifications_reminder FOREIGN KEY (reminder_id) REFERENCES reminders (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
