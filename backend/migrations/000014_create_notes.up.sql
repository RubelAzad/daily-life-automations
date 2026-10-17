CREATE TABLE IF NOT EXISTS notes (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  project_id BIGINT UNSIGNED NULL,
  title VARCHAR(255) NOT NULL,
  body MEDIUMTEXT NULL,
  kind ENUM('note','idea','link','document','receipt') NOT NULL DEFAULT 'note',
  url VARCHAR(2048) NULL,
  pinned BOOLEAN NOT NULL DEFAULT FALSE,
  archived BOOLEAN NOT NULL DEFAULT FALSE,
  expires_on DATE NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_notes_user_updated (user_id, archived, updated_at),
  INDEX idx_notes_user_expires (user_id, expires_on),
  CONSTRAINT fk_notes_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_notes_project FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
