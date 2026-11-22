-- acme turns on conversations from "No" votes that leave an address; the reader's current "No" on its
-- first article names the conversation it opened.
UPDATE help_centers SET feedback_conversations = true WHERE workspace_id = md5('ws-acme')::uuid;

UPDATE kb_article_votes SET conversation_id = md5('conv-acme-1')::uuid WHERE id = md5('kbv-acme-2')::uuid;
