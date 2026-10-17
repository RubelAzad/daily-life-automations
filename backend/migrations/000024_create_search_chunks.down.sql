ALTER TABLE notes DROP COLUMN indexed_at;
ALTER TABLE files
  DROP INDEX idx_files_text_status,
  DROP COLUMN text_claimed_at,
  DROP COLUMN text_chars,
  DROP COLUMN text_error,
  DROP COLUMN text_status;
DROP TABLE IF EXISTS search_chunks;
