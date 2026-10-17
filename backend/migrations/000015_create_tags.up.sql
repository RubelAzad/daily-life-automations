-- Tag names are unique per user, case-insensitively (utf8mb4_unicode_ci).
CREATE TABLE IF NOT EXISTS tags (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(50) NOT NULL,
  color CHAR(7) NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uq_tags_user_name (user_id, name),
  CONSTRAINT fk_tags_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS note_tags (
  note_id BIGINT UNSIGNED NOT NULL,
  tag_id BIGINT UNSIGNED NOT NULL,
  PRIMARY KEY (note_id, tag_id),
  INDEX idx_note_tags_tag (tag_id),
  CONSTRAINT fk_note_tags_note FOREIGN KEY (note_id) REFERENCES notes (id) ON DELETE CASCADE,
  CONSTRAINT fk_note_tags_tag FOREIGN KEY (tag_id) REFERENCES tags (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
