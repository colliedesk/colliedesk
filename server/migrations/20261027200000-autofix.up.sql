-- Autofix, the bridge version: a person sends a conversation to a coding agent the workspace already
-- uses (Claude through an issue, Copilot through an issue, Cursor through its API), on a private GitHub
-- repository the workspace connected through the Colliedesk GitHub App, and follows it to a pull
-- request, its merge and the release. Colliedesk runs no agent and holds no model key; the only vendor
-- key it keeps is Cursor's, sealed. Every move of a fix attempt is an event in fix_attempt_events.

-- A GitHub App installation the workspace connected. installation_id is GitHub's, unique on the server:
-- one installation belongs to one workspace, and a webhook acts only in that workspace. An installation
-- that can reach every repository of its account stays blocked until it is narrowed to selected ones.
CREATE TABLE autofix_github_installations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    installation_id bigint NOT NULL UNIQUE CHECK (installation_id > 0),
    account_login text NOT NULL CHECK (char_length(account_login) BETWEEN 1 AND 100),
    account_type text NOT NULL CHECK (account_type IN ('User', 'Organization')),
    repository_selection text NOT NULL CHECK (repository_selection IN ('selected', 'all')),
    status text NOT NULL CHECK (status IN ('active', 'blocked_all_repositories', 'suspended')),
    connected_by_agent_id uuid,
    connected_by_name text NOT NULL CHECK (char_length(connected_by_name) <= 200),
    connected_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, installation_id),
    FOREIGN KEY (workspace_id, connected_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (connected_by_agent_id),
    -- Reaching every repository of the account never counts as active.
    CHECK (status <> 'blocked_all_repositories' OR repository_selection = 'all'),
    CHECK (repository_selection = 'selected' OR status <> 'active')
);

-- A repository the workspace connected for fixes. Only private ones: a repository that turns public, or
-- leaves the installation, is paused with the reason and takes no new fix. Removing the installation
-- keeps the repository (paused) so its fixes still show where they went.
CREATE TABLE autofix_repos (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    installation_id bigint,
    github_repo_id bigint NOT NULL CHECK (github_repo_id > 0),
    full_name text NOT NULL CHECK (full_name ~ '^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$'),
    visibility text NOT NULL CHECK (visibility IN ('private', 'internal', 'public')),
    visibility_checked_at timestamptz NOT NULL DEFAULT now(),
    default_branch text NOT NULL CHECK (char_length(default_branch) BETWEEN 1 AND 255),
    -- Internal notes go with a fix only when the repository allows it; off until an admin turns it on.
    include_notes boolean NOT NULL DEFAULT false,
    default_connector text CHECK (default_connector IN ('claude_issue', 'copilot_issue', 'cursor_api')),
    paused_reason text CHECK (paused_reason IN ('became_public', 'removed_from_app', 'installation_suspended')),
    paused_at timestamptz,
    connected_by_agent_id uuid,
    connected_by_name text NOT NULL CHECK (char_length(connected_by_name) <= 200),
    connected_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, github_repo_id),
    FOREIGN KEY (workspace_id, installation_id) REFERENCES autofix_github_installations(workspace_id, installation_id) ON DELETE SET NULL (installation_id),
    FOREIGN KEY (workspace_id, connected_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (connected_by_agent_id),
    CHECK ((paused_reason IS NULL) = (paused_at IS NULL))
);
CREATE INDEX autofix_repos_installation ON autofix_repos(workspace_id, installation_id) WHERE installation_id IS NOT NULL;

-- A coding agent the workspace turned on, with the data consent the admin gave (which version of the
-- text, who, when). Only Cursor keeps a key: sealed with tokenseal under the workspace's context, never
-- shown again, not one character.
CREATE TABLE autofix_connectors (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('claude_issue', 'copilot_issue', 'cursor_api')),
    enabled boolean NOT NULL DEFAULT false,
    -- Copilot only: assigned through the API, or assigned by a person on GitHub (when the API refuses the
    -- App's permissions).
    mode text CHECK (mode IN ('assign_api', 'assign_on_github')),
    consent_agent_id uuid,
    consent_name text CHECK (char_length(consent_name) <= 200),
    consent_at timestamptz,
    consent_version text CHECK (char_length(consent_version) BETWEEN 1 AND 40),
    credential_key_id text,
    credential_sealed bytea,
    credential_set_by_agent_id uuid,
    credential_set_by_name text CHECK (char_length(credential_set_by_name) <= 200),
    credential_set_at timestamptz,
    last_check_at timestamptz,
    -- ok, or why the last check failed (key_invalid, no_repo_access, …); the API names the values.
    last_check_result text CHECK (last_check_result ~ '^[a-z_]{1,40}$'),
    last_check_message text CHECK (char_length(last_check_message) <= 500),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, kind),
    FOREIGN KEY (workspace_id, consent_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (consent_agent_id),
    FOREIGN KEY (workspace_id, credential_set_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (credential_set_by_agent_id),
    -- Turned on only with a consent on record.
    CHECK (NOT enabled OR (consent_at IS NOT NULL AND consent_version IS NOT NULL AND consent_name IS NOT NULL)),
    CHECK ((consent_at IS NULL) = (consent_version IS NULL)),
    CHECK ((credential_sealed IS NULL) = (credential_key_id IS NULL)),
    CHECK ((credential_sealed IS NULL) = (credential_set_at IS NULL)),
    CHECK (kind = 'cursor_api' OR credential_sealed IS NULL),
    CHECK (mode IS NULL OR kind = 'copilot_issue'),
    CHECK ((last_check_at IS NULL) = (last_check_result IS NULL))
);

-- One fix attempt: a conversation sent to one coding agent on one repository. A retry is a new attempt
-- that names the one before. The names follow the runner version of Autofix, which only adds columns.
CREATE TABLE fix_attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL,
    attempt_no integer NOT NULL CHECK (attempt_no >= 1),
    previous_attempt_id uuid,
    -- The repository, and its name when it was sent: disconnecting it keeps the attempt readable.
    repo_id uuid,
    repo_full_name text NOT NULL CHECK (repo_full_name ~ '^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$'),
    connector text NOT NULL CHECK (connector IN ('claude_issue', 'copilot_issue', 'cursor_api')),

    state text NOT NULL CHECK (state IN ('sending', 'sent', 'working', 'pr_open', 'merged', 'deployed', 'failed', 'cancelled')),
    outcome_reason text,
    state_changed_at timestamptz NOT NULL DEFAULT now(),
    row_version integer NOT NULL DEFAULT 1 CHECK (row_version >= 1),

    -- Exactly what left Colliedesk: the title and body after masking and the person's edits, and the
    -- messages it was built from. Erased with the conversation.
    sent_title text NOT NULL CHECK (char_length(sent_title) BETWEEN 1 AND 256),
    sent_body text NOT NULL CHECK (char_length(sent_body) BETWEEN 1 AND 65536),
    sent_message_ids uuid[] NOT NULL DEFAULT '{}',
    sent_notes boolean NOT NULL DEFAULT false,
    -- The sender's idempotency key: the same key sends once.
    client_key text CHECK (char_length(client_key) BETWEEN 1 AND 100),

    created_by_kind text NOT NULL CHECK (created_by_kind IN ('person', 'token')),
    created_by_agent_id uuid,
    created_by_name text NOT NULL CHECK (char_length(created_by_name) <= 200),
    created_by_token_id uuid,
    approved_request_id uuid,
    -- The GitHub account that created the issue (no secret); empty for Cursor.
    github_user_id bigint,
    github_login text CHECK (char_length(github_login) BETWEEN 1 AND 100),
    -- Copilot only: assigned to Copilot through the API, or left for a person to assign on GitHub.
    copilot_assignment text CHECK (copilot_assignment IN ('assigned', 'on_github')),

    issue_number integer CHECK (issue_number > 0),
    issue_node_id text CHECK (char_length(issue_node_id) <= 100),
    vendor_ref text CHECK (char_length(vendor_ref) <= 200),
    vendor_run_id text CHECK (char_length(vendor_run_id) <= 200),
    vendor_url text CHECK (vendor_url ~ '^https://'),
    branch text CHECK (char_length(branch) <= 255),
    pr_number integer CHECK (pr_number > 0),
    pr_url text CHECK (pr_url ~ '^https://'),
    pr_linked_by text CHECK (pr_linked_by IN ('auto', 'person')),
    merge_sha text CHECK (merge_sha ~ '^[0-9a-f]{40}$'),
    merged_at timestamptz,
    merged_by_login text CHECK (char_length(merged_by_login) <= 100),
    deployed_at timestamptz,
    deployed_by_agent_id uuid,
    deployed_by_name text CHECK (char_length(deployed_by_name) <= 200),

    dispatch_started_at timestamptz,
    next_check_at timestamptz,
    check_failures integer NOT NULL DEFAULT 0 CHECK (check_failures >= 0),

    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, conversation_id, attempt_no),
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, previous_attempt_id) REFERENCES fix_attempts(workspace_id, id) ON DELETE SET NULL (previous_attempt_id),
    FOREIGN KEY (workspace_id, repo_id) REFERENCES autofix_repos(workspace_id, id) ON DELETE SET NULL (repo_id),
    FOREIGN KEY (workspace_id, created_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (created_by_agent_id),
    FOREIGN KEY (workspace_id, deployed_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (deployed_by_agent_id),
    FOREIGN KEY (workspace_id, approved_request_id) REFERENCES agent_requests(workspace_id, id) ON DELETE SET NULL (approved_request_id),
    -- Failed and cancelled say why, with a reason of their own; every other state has none.
    CHECK (CASE state
        WHEN 'failed' THEN outcome_reason IN ('vendor_auth', 'vendor_repo_access', 'vendor_rejected', 'provider_unavailable',
            'no_repo_access', 'copilot_unavailable', 'repo_public', 'pr_closed', 'agent_error', 'timed_out', 'interrupted')
        WHEN 'cancelled' THEN outcome_reason IN ('cancelled_by_person', 'authorization_abandoned', 'authorization_denied',
            'repo_disconnected', 'conversation_deleted', 'issue_closed', 'connector_disabled')
        ELSE outcome_reason IS NULL END),
    CHECK (state NOT IN ('merged', 'deployed') OR merged_at IS NOT NULL),
    CHECK ((state = 'deployed') = (deployed_at IS NOT NULL)),
    CHECK (state <> 'pr_open' OR pr_url IS NOT NULL),
    CHECK ((pr_url IS NULL) = (pr_linked_by IS NULL)),
    CHECK ((created_by_kind = 'token') = (created_by_token_id IS NOT NULL)),
    CHECK ((github_user_id IS NULL) = (github_login IS NULL)),
    CHECK (copilot_assignment IS NULL OR connector = 'copilot_issue')
);
-- One attempt still in progress per conversation: a new one waits until it ends.
CREATE UNIQUE INDEX fix_attempts_one_in_progress ON fix_attempts(workspace_id, conversation_id)
    WHERE state IN ('sending', 'sent', 'working', 'pr_open');
CREATE UNIQUE INDEX fix_attempts_client_key ON fix_attempts(workspace_id, client_key) WHERE client_key IS NOT NULL;
CREATE INDEX fix_attempts_list ON fix_attempts(workspace_id, created_at DESC, id DESC);
CREATE INDEX fix_attempts_conversation ON fix_attempts(workspace_id, conversation_id, attempt_no DESC);
CREATE INDEX fix_attempts_repo ON fix_attempts(workspace_id, repo_id) WHERE repo_id IS NOT NULL;
CREATE INDEX fix_attempts_issue ON fix_attempts(workspace_id, repo_id, issue_number) WHERE issue_number IS NOT NULL;
CREATE INDEX fix_attempts_pr ON fix_attempts(workspace_id, repo_id, pr_number) WHERE pr_number IS NOT NULL;
-- The worker's sweep: attempts in progress whose next check is due, over every workspace.
CREATE INDEX fix_attempts_due ON fix_attempts(next_check_at)
    WHERE next_check_at IS NOT NULL AND state IN ('sending', 'sent', 'working', 'pr_open');

-- Every move of an attempt and every signal about it, in order: the timeline on the conversation.
-- Append-only; it goes only with its attempt (the conversation or the workspace deleted).
CREATE TABLE fix_attempt_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    fix_attempt_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind ~ '^[a-z_]{1,40}$'),
    from_state text CHECK (from_state IN ('sending', 'sent', 'working', 'pr_open', 'merged', 'deployed', 'failed', 'cancelled')),
    to_state text CHECK (to_state IN ('sending', 'sent', 'working', 'pr_open', 'merged', 'deployed', 'failed', 'cancelled')),
    actor_kind text NOT NULL CHECK (actor_kind IN ('person', 'token', 'github', 'cursor', 'system')),
    actor_id text CHECK (char_length(actor_id) <= 100),
    actor_name text CHECK (char_length(actor_name) <= 200),
    data jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(data) = 'object'),
    -- The webhook delivery or poll that brought it, so a repeated delivery is seen as one.
    delivery_id text CHECK (char_length(delivery_id) <= 100),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, fix_attempt_id) REFERENCES fix_attempts(workspace_id, id) ON DELETE CASCADE,
    CHECK ((from_state IS NULL) = (to_state IS NULL))
);
CREATE INDEX fix_attempt_events_attempt ON fix_attempt_events(workspace_id, fix_attempt_id, id);

CREATE FUNCTION fix_attempt_events_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'fix_attempt_events is append-only';
END
$$;
CREATE TRIGGER fix_attempt_events_no_update BEFORE UPDATE ON fix_attempt_events
    FOR EACH ROW EXECUTE FUNCTION fix_attempt_events_append_only();

-- A GitHub authorization in flight (installing the App, or sending a fix as the person): the hash of its
-- state, who started it and the PKCE verifier. Used once, within 10 minutes, by the same person in the
-- same workspace. Its own table: mailbox_oauth_states is tied to email channels.
CREATE TABLE autofix_oauth_states (
    state_hash bytea PRIMARY KEY CHECK (octet_length(state_hash) = 32),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id uuid NOT NULL,
    purpose text NOT NULL CHECK (purpose IN ('install', 'dispatch')),
    fix_attempt_id uuid,
    code_verifier text NOT NULL CHECK (char_length(code_verifier) BETWEEN 43 AND 128),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id, agent_id) REFERENCES agents(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, fix_attempt_id) REFERENCES fix_attempts(workspace_id, id) ON DELETE CASCADE,
    CHECK ((purpose = 'dispatch') = (fix_attempt_id IS NOT NULL))
);
CREATE INDEX autofix_oauth_states_workspace ON autofix_oauth_states(workspace_id, expires_at);

-- Webhook deliveries already applied, so a repeated one has no second effect (GitHub signs no
-- timestamp). Platform level like retention_deletions: no workspace, no grant to the app role; kept 30
-- days.
CREATE TABLE autofix_webhook_deliveries (
    source text NOT NULL CHECK (source IN ('github')),
    delivery_id text NOT NULL CHECK (char_length(delivery_id) BETWEEN 1 AND 100),
    received_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source, delivery_id)
);
CREATE INDEX autofix_webhook_deliveries_received ON autofix_webhook_deliveries(received_at);

DO $$
DECLARE
    app_role text;
    table_name text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    FOREACH table_name IN ARRAY ARRAY['autofix_github_installations', 'autofix_repos', 'autofix_connectors',
        'fix_attempts', 'fix_attempt_events', 'autofix_oauth_states'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', table_name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', table_name);
        EXECUTE format($policy$CREATE POLICY workspace_isolation ON %2$I FOR ALL TO %1$I
            USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
            WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, app_role, table_name);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %2$I FOR ALL TO %1$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user, table_name);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO %I', table_name, app_role);
    END LOOP;
    -- Events are written and read, never changed: the app role cannot update or delete them (they go
    -- by cascade, which runs as the owner).
    EXECUTE format('REVOKE UPDATE, DELETE ON fix_attempt_events FROM %I', app_role);
    -- Same isolation as retention_deletions: only a privileged transaction reads or writes it.
    ALTER TABLE autofix_webhook_deliveries ENABLE ROW LEVEL SECURITY;
    ALTER TABLE autofix_webhook_deliveries FORCE ROW LEVEL SECURITY;
    EXECUTE format('REVOKE ALL ON autofix_webhook_deliveries FROM %I', app_role);
    EXECUTE format($policy$CREATE POLICY privileged_access ON autofix_webhook_deliveries FOR ALL TO %1$I
        USING ((SELECT current_setting('app.privileged', true) = 'on'))
        WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, current_user);
END
$$;
