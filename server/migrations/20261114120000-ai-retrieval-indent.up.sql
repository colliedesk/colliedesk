-- A past conversation's text now indents every line of a message after its first, so a customer cannot
-- write a line "Reply:" that reads as the team's answer to an AI draft. Build every resolved or closed
-- conversation again.
INSERT INTO ai_retrieval_pending(workspace_id, conversation_id)
    SELECT workspace_id, id FROM conversations WHERE status IN ('resolved', 'closed');
