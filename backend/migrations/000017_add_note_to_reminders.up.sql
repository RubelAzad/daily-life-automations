-- Note reminders fire offset_minutes before the note's expiry (expires_on).
ALTER TABLE reminders
  ADD COLUMN note_id BIGINT UNSIGNED NULL AFTER event_id,
  ADD INDEX idx_reminders_note (note_id),
  ADD CONSTRAINT fk_reminders_note FOREIGN KEY (note_id) REFERENCES notes (id) ON DELETE CASCADE;
