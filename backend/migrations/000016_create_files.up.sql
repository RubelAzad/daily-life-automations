-- File metadata. Contents live in the file store under storage_key, a random
-- server-generated path; original_name is display-only and never used on disk.
CREATE TABLE IF NOT EXISTS files (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  note_id BIGINT UNSIGNED NOT NULL,
  original_name VARCHAR(255) NOT NULL,
  storage_key VARCHAR(255) NOT NULL,
  content_type VARCHAR(100) NOT NULL,
  size_bytes BIGINT UNSIGNED NOT NULL,
  sha256 CHAR(64) NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uq_files_storage_key (storage_key),
  INDEX idx_files_note (note_id),
  CONSTRAINT fk_files_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_files_note FOREIGN KEY (note_id) REFERENCES notes (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
