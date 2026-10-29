-- AI chat (docs/internals/ai-chat.md): end users ask the help center, an AI answers from the published
-- articles. How visitors reach it and how they hand a question to the team is a channel type,
-- help_chat; the AI part is a feature slot, chat, with its own money bucket. Transcripts belong to the
-- workspace: exported, kept 90 days after the last turn, deletable one by one.

INSERT INTO channel_types (type, connectable) VALUES ('help_chat', true);

-- A help chat channel's surface: what the chat says first, the questions it offers, what the admin
-- adds to the AI's instructions and the sites that may embed it. History goes through channel_events.
CREATE TABLE help_chat_channels (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id uuid NOT NULL,
    greeting text NOT NULL DEFAULT '' CHECK (char_length(greeting) <= 300),
    suggestions text[] NOT NULL DEFAULT '{}' CHECK (cardinality(suggestions) <= 4 AND char_length(array_to_string(suggestions, '')) <= 480),
    instructions text NOT NULL DEFAULT '' CHECK (char_length(instructions) <= 2000),
    -- Exact origins (scheme://host[:port]) whose pages may frame the chat.
    widget_origins text[] NOT NULL DEFAULT '{}' CHECK (cardinality(widget_origins) <= 20),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, channel_id),
    FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id) ON DELETE CASCADE
);

ALTER TABLE ai_slots DROP CONSTRAINT ai_slots_slot_check,
    ADD CONSTRAINT ai_slots_slot_check CHECK (slot IN ('triage', 'draft', 'chat')),
    -- The most the slot may spend a calendar month (UTC), in micro-dollars, on a provider the
    -- workspace pays for or a server without plan limits; NULL is no limit. The cloud's limits on its
    -- own provider are in the code, not here.
    ADD COLUMN monthly_cap_micro_usd bigint CHECK (monthly_cap_micro_usd > 0);
-- chat_deleted: an admin deleted a chat's transcript (never its texts here).
ALTER TABLE ai_settings_events DROP CONSTRAINT ai_settings_events_slot_check,
    ADD CONSTRAINT ai_settings_events_slot_check CHECK (slot IN ('triage', 'draft', 'chat')),
    DROP CONSTRAINT ai_settings_events_kind_check,
    ADD CONSTRAINT ai_settings_events_kind_check CHECK (kind IN ('provider_added', 'provider_changed',
        'provider_key_changed', 'provider_removed', 'slot_changed', 'automatic_limits_changed', 'chat_deleted'));
ALTER TABLE ai_usage DROP CONSTRAINT ai_usage_feature_check,
    ADD CONSTRAINT ai_usage_feature_check CHECK (feature IN ('check', 'triage', 'draft', 'chat'));

-- One end user's chat in one browser: the cookie holds a random token, only its hash is stored, and
-- a chat belongs to the channel it started on.
CREATE TABLE ai_chat_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id uuid NOT NULL,
    token_hash bytea NOT NULL CHECK (octet_length(token_hash) = 32),
    -- A chat moved from a site's frame to the help center's own page answers a second token there: the
    -- frame's cookie is partitioned by the site, so it never reaches the page.
    page_token_hash bytea CHECK (octet_length(page_token_hash) = 32),
    -- Where it started: the chat page, the chat box of an article, or a site's widget.
    surface text NOT NULL CHECK (surface IN ('help_center', 'article', 'widget')),
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'ended', 'handed_off')),
    -- The conversation a hand-off opened; it outlives the chat and keeps its own copy.
    conversation_id uuid,
    -- The oldest turns before this moment no longer fit what the AI reads (cut by blocks of ten).
    history_cut_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_turn_at timestamptz NOT NULL DEFAULT now(),
    ended_at timestamptz,
    UNIQUE (workspace_id, id),
    UNIQUE (workspace_id, channel_id, token_hash),
    UNIQUE (workspace_id, channel_id, page_token_hash),
    FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE SET NULL (conversation_id),
    CHECK ((status = 'ended') = (ended_at IS NOT NULL))
);
CREATE INDEX ai_chat_sessions_recent ON ai_chat_sessions(workspace_id, last_turn_at DESC, id DESC);

-- One question and its answer. The texts become NULL when the transcript expires; the numbers stay.
CREATE TABLE ai_chat_turns (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    session_id uuid NOT NULL,
    seq integer NOT NULL CHECK (seq >= 1),
    question text CHECK (char_length(question) <= 1000),
    answer text CHECK (char_length(answer) <= 20000),
    -- The article the end user was reading when they asked, when they asked about it.
    viewing_kb_article_id uuid,
    status text NOT NULL CHECK (status IN ('streaming', 'answered', 'no_source', 'over_limit', 'failed')),
    error_code text CHECK (char_length(error_code) <= 40),
    model text,
    ttft_ms integer,
    input_tokens bigint,
    cache_read_tokens bigint,
    cache_creation_tokens bigint,
    output_tokens bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    UNIQUE (workspace_id, id),
    UNIQUE (session_id, seq),
    FOREIGN KEY (workspace_id, session_id) REFERENCES ai_chat_sessions(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, viewing_kb_article_id) REFERENCES kb_articles(workspace_id, id) ON DELETE SET NULL (viewing_kb_article_id),
    CHECK ((status = 'failed') = (error_code IS NOT NULL))
);
CREATE INDEX ai_chat_turns_month ON ai_chat_turns(workspace_id, created_at);

-- The articles an answer cites, as they were: number, version, title and collection are copied, so
-- a later edit or deletion leaves the transcript as the end user saw it.
CREATE TABLE ai_chat_sources (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    turn_id uuid NOT NULL,
    ordinal integer NOT NULL CHECK (ordinal >= 1),
    kb_article_id uuid,
    article_number bigint NOT NULL,
    article_version integer NOT NULL,
    title text,
    folder text,
    PRIMARY KEY (turn_id, ordinal),
    FOREIGN KEY (workspace_id, turn_id) REFERENCES ai_chat_turns(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, kb_article_id) REFERENCES kb_articles(workspace_id, id) ON DELETE SET NULL (kb_article_id)
);

CREATE TABLE ai_chat_feedback (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    turn_id uuid NOT NULL,
    rating text NOT NULL CHECK (rating IN ('helpful', 'not_helpful')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, turn_id),
    FOREIGN KEY (workspace_id, turn_id) REFERENCES ai_chat_turns(workspace_id, id) ON DELETE CASCADE
);

-- What the AI reads of a workspace's articles, by the fingerprint of the set (ids, versions, "Keep
-- away from AI", titles) and model: its size in that model's tokens, whether it fits whole, and when a
-- call last wrote or read its cache.
CREATE TABLE ai_chat_kb_snapshots (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    fingerprint bytea NOT NULL CHECK (octet_length(fingerprint) = 32),
    model text NOT NULL CHECK (char_length(model) BETWEEN 1 AND 200),
    kb_tokens integer NOT NULL CHECK (kb_tokens >= 0),
    system_tokens integer NOT NULL CHECK (system_tokens >= 0),
    whole boolean NOT NULL,
    last_cache_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, fingerprint, model)
);

-- A one-time code that moves a chat from a site's widget to the full chat page: two minutes, used
-- once, stored as a hash.
CREATE TABLE ai_chat_transfers (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    session_id uuid NOT NULL,
    code_hash bytea NOT NULL CHECK (octet_length(code_hash) = 32),
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    PRIMARY KEY (workspace_id, code_hash),
    FOREIGN KEY (workspace_id, session_id) REFERENCES ai_chat_sessions(workspace_id, id) ON DELETE CASCADE
);

DO $$
DECLARE
    app_role text;
    name text;
BEGIN
    SELECT roles[1]::text INTO app_role FROM pg_policies
        WHERE schemaname = current_schema() AND tablename = 'workspaces' AND policyname = 'workspace_isolation';
    IF app_role IS NULL THEN
        RAISE EXCEPTION 'row-level security is not set up in this schema';
    END IF;
    FOREACH name IN ARRAY ARRAY['help_chat_channels', 'ai_chat_sessions', 'ai_chat_turns', 'ai_chat_sources', 'ai_chat_feedback',
            'ai_chat_kb_snapshots', 'ai_chat_transfers'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', name);
        EXECUTE format($policy$CREATE POLICY workspace_isolation ON %1$I FOR ALL TO %2$I
            USING (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))
            WITH CHECK (workspace_id = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid))$policy$, name, app_role);
        EXECUTE format($policy$CREATE POLICY privileged_access ON %1$I FOR ALL TO %2$I
            USING ((SELECT current_setting('app.privileged', true) = 'on'))
            WITH CHECK ((SELECT current_setting('app.privileged', true) = 'on'))$policy$, name, current_user);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO %I', name, app_role);
    END LOOP;
END
$$;
