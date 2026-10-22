-- AI reply drafts and triage suggestions (docs/internals/ai-drafts.md). When a customer writes, the
-- server queues a draft; a worker reads what AI may quote (ai_retrieval_docs), asks the model for a
-- reply and a triage suggestion, and stores both here. A person sends, edits or discards the draft.
-- Auto-answer sends a draft on its own only where people have already sent most drafts of the same
-- topic unchanged, after a delay during which anyone can stop it.

-- One row per workspace that changed a setting; a workspace without a row has the defaults.
CREATE TABLE workspace_ai_settings (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    drafts_enabled boolean NOT NULL DEFAULT true,
    auto_answer_enabled boolean NOT NULL DEFAULT false,
    -- How long a scheduled answer waits for a person to stop or change it.
    auto_answer_delay_seconds integer NOT NULL DEFAULT 300 CHECK (auto_answer_delay_seconds BETWEEN 0 AND 86400),
    -- A topic answers on its own once at least this many of its drafts were reviewed by a person...
    auto_answer_min_reviewed integer NOT NULL DEFAULT 10 CHECK (auto_answer_min_reviewed BETWEEN 3 AND 1000),
    -- ...and at least this share of them (percent) was sent with no more than small edits.
    auto_answer_min_accuracy integer NOT NULL DEFAULT 90 CHECK (auto_answer_min_accuracy BETWEEN 50 AND 100),
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by_agent_id uuid,
    FOREIGN KEY (workspace_id, updated_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (updated_by_agent_id)
);

CREATE TABLE ai_drafts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL,
    -- The customer message the draft answers; NULL when a person asked for one.
    message_id uuid,
    requested_by_agent_id uuid,
    status text NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'ready', 'failed', 'superseded', 'used', 'discarded', 'scheduled', 'auto_sent', 'cancelled')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    -- What the model proposes: answer the customer, ask them one question, or hand over to a person.
    action text CHECK (action IN ('answer', 'clarify', 'handoff')),
    body text,
    -- Decided by the server from the model's own rating and checks on what it cited.
    confidence text CHECK (confidence IN ('high', 'medium', 'low')),
    confidence_reasons text[] NOT NULL DEFAULT '{}',
    handoff_reason text,
    -- The knowledge base article the reply rests on; auto-answer is earned per article.
    topic_article_id uuid,
    -- [{kind, id, number, title}] of the sources the reply cites, read from the database, never the model.
    sources jsonb NOT NULL DEFAULT '[]',
    -- {tags: [names of existing tags], priority, reason}
    triage jsonb,
    triage_applied_at timestamptz,
    triage_applied_by_agent_id uuid,
    model text,
    error_code text,
    -- What a person did with it: used (sent, see similarity), discarded, cancelled a scheduled answer.
    outcome text CHECK (outcome IN ('sent_unchanged', 'sent_edited', 'discarded', 'auto_sent', 'auto_cancelled')),
    -- How close the sent reply is to the draft, 0 to 1.
    similarity real CHECK (similarity BETWEEN 0 AND 1),
    decided_by_agent_id uuid,
    decided_at timestamptz,
    auto_send_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    ready_at timestamptz,
    UNIQUE (workspace_id, id),
    CHECK (status <> 'scheduled' OR auto_send_at IS NOT NULL),
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, message_id) REFERENCES messages(workspace_id, id) ON DELETE SET NULL (message_id),
    FOREIGN KEY (workspace_id, requested_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (requested_by_agent_id),
    FOREIGN KEY (workspace_id, triage_applied_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (triage_applied_by_agent_id),
    FOREIGN KEY (workspace_id, decided_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (decided_by_agent_id),
    FOREIGN KEY (workspace_id, topic_article_id) REFERENCES kb_articles(workspace_id, id) ON DELETE SET NULL (topic_article_id)
);
-- The latest draft of a conversation is what the conversation screen reads.
CREATE INDEX ai_drafts_conversation_idx ON ai_drafts(workspace_id, conversation_id, created_at DESC);
-- The worker finds queued drafts and scheduled answers that are due.
CREATE INDEX ai_drafts_queued_idx ON ai_drafts(next_attempt_at) WHERE status = 'queued';
CREATE INDEX ai_drafts_scheduled_idx ON ai_drafts(auto_send_at) WHERE status = 'scheduled';
-- A topic's record: reviewed drafts per article.
CREATE INDEX ai_drafts_topic_idx ON ai_drafts(workspace_id, topic_article_id) WHERE outcome IS NOT NULL;

-- A reply sent from a draft, by a person or on its own, names the draft. The column is new, so every
-- existing row is NULL: NOT VALID skips a scan of the whole table, new rows are still checked.
ALTER TABLE messages ADD COLUMN ai_draft_id uuid;
ALTER TABLE messages ADD CONSTRAINT messages_ai_draft_fkey
    FOREIGN KEY (workspace_id, ai_draft_id) REFERENCES ai_drafts(workspace_id, id) ON DELETE SET NULL (ai_draft_id) NOT VALID;

-- One row per call to a provider (an attempt), for billing and limits. Tokens are NULL when the
-- provider did not report them, never 0.
CREATE TABLE ai_usage (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    ai_draft_id uuid,
    feature text NOT NULL CHECK (feature IN ('draft', 'check')),
    model text NOT NULL,
    input_tokens bigint,
    output_tokens bigint,
    cache_read_tokens bigint,
    cache_write_tokens bigint,
    error_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id, ai_draft_id) REFERENCES ai_drafts(workspace_id, id) ON DELETE SET NULL (ai_draft_id)
);
CREATE INDEX ai_usage_workspace_idx ON ai_usage(workspace_id, created_at);

DO $$
DECLARE
    app_role text;
    ai_table text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    FOREACH ai_table IN ARRAY ARRAY['workspace_ai_settings', 'ai_drafts', 'ai_usage'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', ai_table);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', ai_table);
        EXECUTE format($policy$CREATE POLICY workspace_isolation ON %1$I FOR ALL TO %2$I
            USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
            WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, ai_table, app_role);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %1$I FOR ALL TO %2$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, ai_table, current_user);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO %I', ai_table, app_role);
    END LOOP;
END
$$;
