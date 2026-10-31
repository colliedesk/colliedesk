-- Reminder and overdue emails of response targets: one row per conversation, clock, threshold, due time
-- and recipient, so each goes to each person once however many servers or rounds see it due (a send that
-- failed for now gives that person's row back; a claim left 'sending' by a stopped server is taken again
-- after a while). The row with no recipient ('') closes the threshold: done once everyone's row is
-- settled, or nobody when nobody gets these emails. sla_alert_since is when the conversation came in: a threshold before it (an import of what was
-- already due) sends nothing. A teammate turns their emails off with agents.sla_alert_email.
CREATE TABLE conversation_sla_alerts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL,
    clock text NOT NULL CHECK (clock IN ('first_response', 'resolution')),
    threshold text NOT NULL CHECK (threshold IN ('reminder', 'overdue')),
    due_at timestamptz NOT NULL,
    recipient text NOT NULL DEFAULT '' CHECK (char_length(recipient) <= 320),
    status text NOT NULL DEFAULT 'sending' CHECK (status IN ('sending', 'sent', 'rejected', 'done', 'nobody')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, conversation_id, clock, threshold, due_at, recipient),
    CHECK ((recipient = '') = (status IN ('done', 'nobody'))),
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX conversation_sla_alerts_conversation ON conversation_sla_alerts(workspace_id, conversation_id, created_at);
ALTER TABLE agents ADD COLUMN sla_alert_email boolean NOT NULL DEFAULT true;
ALTER TABLE conversations ADD COLUMN sla_alert_since timestamptz;
UPDATE conversations SET sla_alert_since = created_at WHERE sla_applies;
CREATE INDEX conversations_sla_warn ON conversations(sla_warn_at) WHERE sla_warn_at IS NOT NULL;

CREATE OR REPLACE FUNCTION conversations_sla_open() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.sla_applies IS NULL THEN
        SELECT COALESCE(bool_and(t.enabled AND NEW.channel NOT IN ('imported', 'problem_report')
                AND (t.channel_ids IS NULL OR NEW.channel_id = ANY(t.channel_ids))
                AND COALESCE(NEW.created_at, now()) >= t.enabled_at), false)
            INTO NEW.sla_applies FROM response_targets t WHERE t.workspace_id = NEW.workspace_id;
    END IF;
    NEW.sla_stale := NEW.sla_applies;
    NEW.sla_alert_since := COALESCE(NEW.sla_alert_since, now());
    RETURN NEW;
END
$$;

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE conversation_sla_alerts ENABLE ROW LEVEL SECURITY;
    ALTER TABLE conversation_sla_alerts FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON conversation_sla_alerts FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON conversation_sla_alerts FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON conversation_sla_alerts TO %I', app_role);
END
$$;
