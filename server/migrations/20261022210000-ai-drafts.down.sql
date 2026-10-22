-- Drafts are suggestions; the replies sent from them stay, without the link.
DROP TRIGGER ai_drafts_discard_on_resolve ON conversations;
DROP FUNCTION ai_drafts_discard_on_resolve();
ALTER TABLE messages DROP COLUMN ai_draft_id;
DROP TABLE ai_drafts;
