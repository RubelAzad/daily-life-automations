ALTER TABLE reminders
  DROP FOREIGN KEY fk_reminders_note,
  DROP INDEX idx_reminders_note,
  DROP COLUMN note_id;
