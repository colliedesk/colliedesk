-- Removes the help center of channels and conversations and the help chat reader's views and
-- grants. Every channel serves the primary again; the help center a conversation was opened on is
-- forgotten.
DROP VIEW kb_help_center_chat_translations;
DROP VIEW kb_help_center_chat_articles;

SELECT release_help_center_table('kb_article_versions');
SELECT release_help_center_table('ai_retrieval_docs');

DROP INDEX conversations_help_center;
ALTER TABLE conversations DROP COLUMN help_center_id;
DROP INDEX channels_help_center;
ALTER TABLE channels DROP COLUMN help_center_id;
