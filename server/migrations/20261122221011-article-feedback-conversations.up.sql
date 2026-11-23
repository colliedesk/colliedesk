-- A "No" on an article that leaves a line and an email address may open a conversation, when the
-- workspace turns it on (off by default; the knowledge base's setting, read on its primary help
-- center like require_approval). The conversation has an origin and no channel row, like a problem
-- report; the vote names the conversation it opened, so sending the same form again opens no
-- second one, and the conversation going (spam, undo) keeps the vote.
INSERT INTO channel_types (type, connectable) VALUES ('article_feedback', false);

ALTER TABLE help_centers ADD COLUMN feedback_conversations boolean NOT NULL DEFAULT false;

ALTER TABLE kb_article_votes ADD COLUMN conversation_id uuid,
    ADD CONSTRAINT kb_article_votes_conversation_fkey FOREIGN KEY (workspace_id, conversation_id)
        REFERENCES conversations(workspace_id, id) ON DELETE SET NULL (conversation_id);
CREATE INDEX kb_article_votes_conversation_idx ON kb_article_votes(workspace_id, conversation_id)
    WHERE conversation_id IS NOT NULL;
