-- Millisecond precision, so a decision can be ordered against chat messages
-- (ai_messages.created_at) that were written in the same second.
ALTER TABLE ai_actions MODIFY decided_at TIMESTAMP(3) NULL;
