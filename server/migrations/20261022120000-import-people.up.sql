-- The agents of a Freshdesk account an in-app import read (#97), so the team can invite them before or
-- after the import and link each to a Colliedesk agent: their imported replies and the tickets assigned
-- to them then show that agent. A link is applied by the import worker (relink_pending); relink_from
-- is the agent a changed or removed link is taken away from. See docs/internals/freshdesk-import.md.
CREATE TABLE import_people (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    -- The ledger source of the account, for example freshdesk:account:777.
    source text NOT NULL,
    -- The Freshdesk agent id (origin.author_external_id, origin.responder_external_id).
    external_id text NOT NULL,
    name text NOT NULL,
    email text NOT NULL,
    active boolean NOT NULL,
    linked_agent_id uuid,
    -- linked_by_email: the link was made because the emails match (at the preview, or when the
    -- invite was accepted); otherwise a person chose it.
    linked_by_email boolean NOT NULL DEFAULT false,
    relink_pending boolean NOT NULL DEFAULT false,
    relink_from uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, source, external_id),
    FOREIGN KEY (workspace_id, linked_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (linked_agent_id),
    FOREIGN KEY (workspace_id, relink_from) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (relink_from)
);
CREATE INDEX import_people_email ON import_people(workspace_id, lower(email));
CREATE INDEX import_people_pending ON import_people(updated_at) WHERE relink_pending;

DO $$
DECLARE
    app_role text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    ALTER TABLE import_people ENABLE ROW LEVEL SECURITY;
    ALTER TABLE import_people FORCE ROW LEVEL SECURITY;
    EXECUTE format($policy$CREATE POLICY workspace_isolation ON import_people FOR ALL TO %1$I
        USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
        WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON import_people FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON import_people TO %I', app_role);
END
$$;
