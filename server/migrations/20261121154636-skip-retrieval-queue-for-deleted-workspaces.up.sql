-- Deleting a workspace cascades to the files its articles list, and the statement trigger then runs
-- after the workspace row is gone: there is nothing left to queue. Queueing anyway fails the delete,
-- on the foreign key to workspaces (app.privileged) or on row-level security (a superuser, whose
-- cascade runs as the table owner). Queueing only for a workspace the trigger can see passes both:
-- the queue's policies match those of workspaces, so a workspace the check sees is one the insert may name.
CREATE OR REPLACE FUNCTION ai_retrieval_attachment_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO ai_retrieval_pending(workspace_id, kb_article_id)
        SELECT DISTINCT c.workspace_id, c.article_id FROM changed c
        WHERE EXISTS (SELECT 1 FROM workspaces w WHERE w.id = c.workspace_id);
    RETURN NULL;
END
$$;
