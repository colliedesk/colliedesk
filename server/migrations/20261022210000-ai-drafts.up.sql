-- AI reply drafts (#89). A person asks for a draft with an optional prompt; a background task
-- writes it from the conversation, matching knowledge base articles and similar solved tickets; the
-- person edits it and sends it, or not. The AI never sends. How each draft ended (sent nearly as
-- written, sent edited, discarded, replaced by a newer one, expired) is kept for the team's numbers
-- (#100).
CREATE TABLE ai_drafts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL,
    -- The task that writes it; gone after a restore, which reads as a failed draft.
    task_id uuid,
    requested_by_agent_id uuid,
    requested_by_token_id uuid,
    prompt text NOT NULL DEFAULT '' CHECK (char_length(prompt) <= 2000),
    -- pending while its task runs (the task says queued, running, failed or cancelled), ready once
    -- written, cancelled when the person stopped it before it was ready.
    status text NOT NULL CHECK (status IN ('pending', 'ready', 'cancelled')),
    text text,
    missing_info text,
    -- The sources the draft used, as the database names them: kind, id, number, title.
    sources jsonb NOT NULL DEFAULT '[]',
    -- Addresses, numbers or names in the draft that the conversation does not hold.
    warnings jsonb NOT NULL DEFAULT '[]',
    model text,
    outcome text CHECK (outcome IN ('sent_unchanged', 'sent_edited', 'discarded', 'superseded',
        'expired')),
    -- How close the sent reply was to the draft, 0 to 1 (words); sent_unchanged is 0.9 and above.
    similarity numeric(4, 3) CHECK (similarity BETWEEN 0 AND 1),
    sent_message_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    ready_at timestamptz,
    decided_at timestamptz,
    decided_by_agent_id uuid,
    -- The text and the prompt are erased 90 days after the outcome; the numbers stay.
    text_cleared_at timestamptz,
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id)
        ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, task_id) REFERENCES ai_tasks(workspace_id, id) ON DELETE
        SET NULL (task_id),
    FOREIGN KEY (workspace_id, requested_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE
        SET NULL (requested_by_agent_id),
    FOREIGN KEY (workspace_id, decided_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE
        SET NULL (decided_by_agent_id),
    FOREIGN KEY (workspace_id, sent_message_id) REFERENCES messages(workspace_id, id) ON DELETE
        SET NULL (sent_message_id),
    CHECK ((status = 'ready') = (ready_at IS NOT NULL)),
    CHECK (status = 'ready' OR outcome IS NULL),
    CHECK ((outcome IS NULL) = (decided_at IS NULL)),
    CHECK ((outcome IN ('sent_unchanged', 'sent_edited')) = (similarity IS NOT NULL))
);
CREATE INDEX ai_drafts_conversation ON ai_drafts(workspace_id, conversation_id, created_at DESC);
CREATE INDEX ai_drafts_task ON ai_drafts(workspace_id, task_id) WHERE task_id IS NOT NULL;
-- The worker's sweep: undecided ready drafts to expire, ended ones whose text is due to go (a draft
-- ends when decided, or at once when it never became ready).
CREATE INDEX ai_drafts_undecided ON ai_drafts(ready_at) WHERE status = 'ready' AND outcome IS NULL;
CREATE INDEX ai_drafts_text_due ON ai_drafts((COALESCE(decided_at, created_at)))
    WHERE text_cleared_at IS NULL AND (decided_at IS NOT NULL OR status <> 'ready');

-- The knowledge base articles a draft used, for "used in N drafts this month" on the article: a
-- containment search on ai_drafts.sources cannot use an index under row-level security.
CREATE TABLE ai_draft_sources (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    draft_id uuid NOT NULL,
    kb_article_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id, draft_id, kb_article_id),
    FOREIGN KEY (workspace_id, draft_id) REFERENCES ai_drafts(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, kb_article_id) REFERENCES kb_articles(workspace_id, id)
        ON DELETE CASCADE
);
CREATE INDEX ai_draft_sources_article ON ai_draft_sources(workspace_id, kb_article_id, created_at);

-- A reply sent from a draft names it, so the timeline can say so and the outcome is exact.
ALTER TABLE messages ADD COLUMN ai_draft_id uuid;
ALTER TABLE messages ADD FOREIGN KEY (workspace_id, ai_draft_id)
    REFERENCES ai_drafts(workspace_id, id) ON DELETE SET NULL (ai_draft_id);

-- A conversation resolved or closed discards the drafts nobody decided on, and stops the ones still
-- being written (their tasks too): no call is spent on a reply nobody will send.
CREATE FUNCTION ai_drafts_discard_on_resolve() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE ai_drafts SET outcome = 'discarded', decided_at = now()
        WHERE workspace_id = NEW.workspace_id AND conversation_id = NEW.id AND status = 'ready' AND outcome IS NULL;
    WITH stopped AS (
        UPDATE ai_drafts SET status = 'cancelled'
            WHERE workspace_id = NEW.workspace_id AND conversation_id = NEW.id AND status = 'pending' RETURNING task_id)
    UPDATE ai_tasks SET state = 'cancelled', error_code = 'conversation_closed', claim_token = NULL, lease_until = NULL, finished_at = now()
        WHERE workspace_id = NEW.workspace_id AND id IN (SELECT task_id FROM stopped) AND state IN ('queued', 'running');
    RETURN NULL;
END
$$;
CREATE TRIGGER ai_drafts_discard_on_resolve AFTER UPDATE OF status ON conversations
    FOR EACH ROW WHEN (NEW.status IN ('resolved', 'closed') AND OLD.status IS DISTINCT
        FROM NEW.status)
    EXECUTE FUNCTION ai_drafts_discard_on_resolve();

SELECT secure_workspace_table('ai_draft_sources');
SELECT secure_workspace_table('ai_drafts');
