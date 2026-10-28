-- AI triage suggestions (#89). A new ticket's first customer message queues an automatic task that
-- suggests tags, a priority and an assignee; a person accepts each part, all of it, or none. Nothing
-- is applied until someone accepts. An imported message (origin set) queues nothing.
CREATE TABLE ai_triage_suggestions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL,
    -- The task that writes it; gone after a restore, which reads as a failed suggestion.
    task_id uuid,
    automatic boolean NOT NULL DEFAULT false,
    requested_by_agent_id uuid,
    requested_by_token_id uuid,
    status text NOT NULL CHECK (status IN ('pending', 'ready', 'cancelled')),
    -- Only tags and agents of the workspace, checked when the answer is read.
    tag_ids uuid[] NOT NULL DEFAULT '{}',
    priority text CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    assignee_agent_id uuid,
    reason text,
    -- The articles and solved tickets the suggestion read, as the database names them: kind, id, title.
    sources jsonb NOT NULL DEFAULT '[]',
    -- The conversation's priority and assignee when the suggestion was asked for. A field a person
    -- changed before the answer came is not suggested: stale holds what they set instead.
    base jsonb NOT NULL DEFAULT '{}',
    stale jsonb NOT NULL DEFAULT '{}',
    model text,
    outcome text CHECK (outcome IN ('accepted', 'partly_accepted', 'dismissed', 'superseded', 'expired')),
    -- What was applied so far: {"tag_ids": [...], "priority": "...", "assignee_agent_id": "..."}. A part
    -- applied leaves the rest open; the outcome is set when all of it is applied, or the rest is
    -- dismissed, replaced or expires (partly_accepted when some was applied).
    applied jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    ready_at timestamptz,
    decided_at timestamptz,
    decided_by_agent_id uuid,
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, task_id) REFERENCES ai_tasks(workspace_id, id) ON DELETE SET NULL (task_id),
    FOREIGN KEY (workspace_id, requested_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (requested_by_agent_id),
    FOREIGN KEY (workspace_id, assignee_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (assignee_agent_id),
    FOREIGN KEY (workspace_id, decided_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (decided_by_agent_id),
    CHECK ((status = 'ready') = (ready_at IS NOT NULL)),
    CHECK (status = 'ready' OR outcome IS NULL),
    CHECK ((outcome IS NULL) = (decided_at IS NULL))
);
CREATE INDEX ai_triage_conversation ON ai_triage_suggestions(workspace_id, conversation_id, created_at DESC);
CREATE INDEX ai_triage_task ON ai_triage_suggestions(workspace_id, task_id) WHERE task_id IS NOT NULL;
-- The sweeps: an open suggestion with a part to accept expires after a week (one naming nothing never
-- does), and a reason is erased 90 days after the suggestion ended.
CREATE INDEX ai_triage_undecided ON ai_triage_suggestions(ready_at) WHERE status = 'ready' AND outcome IS NULL
    AND (tag_ids <> '{}' OR priority IS NOT NULL OR assignee_agent_id IS NOT NULL);
CREATE INDEX ai_triage_reason_due ON ai_triage_suggestions((COALESCE(decided_at, created_at))) WHERE reason IS NOT NULL;

-- The first customer message of a conversation queues a suggestion when triage is on. Every way a
-- conversation starts (email, web form, the API) inserts that message; an import sets origin.
CREATE FUNCTION ai_triage_on_first_message() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    task uuid;
BEGIN
    IF NEW.kind <> 'incoming' OR NEW.origin IS NOT NULL THEN
        RETURN NULL;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM ai_slots WHERE workspace_id = NEW.workspace_id AND slot = 'triage' AND enabled) THEN
        RETURN NULL;
    END IF;
    IF EXISTS (SELECT 1 FROM messages WHERE workspace_id = NEW.workspace_id AND conversation_id = NEW.conversation_id
            AND kind = 'incoming' AND id <> NEW.id) THEN
        RETURN NULL;
    END IF;
    INSERT INTO ai_tasks(workspace_id, kind, conversation_id, automatic, state)
        VALUES (NEW.workspace_id, 'triage', NEW.conversation_id, true, 'queued') RETURNING id INTO task;
    INSERT INTO ai_triage_suggestions(workspace_id, conversation_id, task_id, automatic, status, base)
        SELECT NEW.workspace_id, NEW.conversation_id, task, true, 'pending',
            jsonb_build_object('priority', c.priority, 'assignee_agent_id', c.assignee_id)
        FROM conversations c WHERE c.workspace_id = NEW.workspace_id AND c.id = NEW.conversation_id;
    RETURN NULL;
END
$$;
CREATE TRIGGER ai_triage_on_first_message AFTER INSERT ON messages
    FOR EACH ROW WHEN (NEW.kind = 'incoming' AND NEW.origin IS NULL)
    EXECUTE FUNCTION ai_triage_on_first_message();

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE ai_triage_suggestions ENABLE ROW LEVEL SECURITY;
    ALTER TABLE ai_triage_suggestions FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON ai_triage_suggestions FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON ai_triage_suggestions FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON ai_triage_suggestions TO %I', app_role);
END
$$;
