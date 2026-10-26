-- M4-R1 additive indexes. Review SHOW INDEX FROM messages before applying;
-- these statements are an explicit deployment step, never request-path DDL.
-- Execute once against the Messenger-owned database with an approved window.
-- Reverting application code does not require removing these indexes.
CREATE INDEX idx_messages_conversation_id ON messages (conversation_id, id);
CREATE INDEX idx_messages_unread_page ON messages (receiver_id, is_read, conversation_id, id);
