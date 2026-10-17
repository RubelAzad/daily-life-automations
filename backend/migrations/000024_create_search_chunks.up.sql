-- Full-text search over notes and the text inside their attachments.
-- Text is split into chunks of about 1 500 characters, so a match points to
-- a passage rather than a whole document. Chunks go when their note or file
-- is deleted.
CREATE TABLE IF NOT EXISTS search_chunks (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  note_id BIGINT UNSIGNED NOT NULL,
  file_id BIGINT UNSIGNED NULL,           -- NULL: the note's own text
  chunk_index INT UNSIGNED NOT NULL,
  title VARCHAR(255) NOT NULL,            -- note title, or file name
  content MEDIUMTEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FULLTEXT KEY ft_search_chunks (title, content),
  INDEX idx_search_chunks_user (user_id),
  INDEX idx_search_chunks_note (note_id, file_id),
  CONSTRAINT fk_search_chunks_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_search_chunks_note FOREIGN KEY (note_id) REFERENCES notes (id) ON DELETE CASCADE,
  CONSTRAINT fk_search_chunks_file FOREIGN KEY (file_id) REFERENCES files (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Text extraction state of each attachment. Existing files start as
-- "pending", so the worker indexes them too.
ALTER TABLE files
  ADD COLUMN text_status VARCHAR(12) NOT NULL DEFAULT 'pending' AFTER sha256, -- pending | processing | ready | unsupported | failed
  ADD COLUMN text_error VARCHAR(255) NULL AFTER text_status,
  ADD COLUMN text_chars INT UNSIGNED NULL AFTER text_error,
  ADD COLUMN text_claimed_at TIMESTAMP NULL AFTER text_chars,
  ADD INDEX idx_files_text_status (text_status);

-- When a note's own text was last indexed; NULL or older than updated_at
-- means it needs (re)indexing.
ALTER TABLE notes ADD COLUMN indexed_at TIMESTAMP(3) NULL;
