ALTER TABLE ai_actions
  DROP FOREIGN KEY fk_ai_actions_conversation,
  DROP INDEX idx_ai_actions_conversation,
  DROP COLUMN conversation_id;
DROP TABLE IF EXISTS ai_messages;
DROP TABLE IF EXISTS ai_conversations;
