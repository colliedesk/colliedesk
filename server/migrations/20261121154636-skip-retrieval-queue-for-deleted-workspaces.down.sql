CREATE OR REPLACE FUNCTION ai_retrieval_attachment_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO ai_retrieval_pending(workspace_id, kb_article_id)
        SELECT DISTINCT workspace_id, article_id FROM changed;
    RETURN NULL;
END
$$;
