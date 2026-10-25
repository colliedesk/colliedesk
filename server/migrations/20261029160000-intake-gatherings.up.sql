-- The intake of chat channels (docs/internals/channels.md "Intake"): what a customer said in one go
-- on a scope that collects messages is a gathering, decided as one, by the channel's rules, by AI where
-- the rules only had a default, or by a person in the queue. Email and web forms have none.
CREATE TABLE intake_gatherings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    channel_id uuid NOT NULL,
    scope text NOT NULL DEFAULT '' CHECK (scope ~ '^[a-z_]{0,40}$'),
    -- Where gatherings are decided one after the other: a thread and its sender ("thread:<id>:<sender>").
    lane_key text NOT NULL CHECK (char_length(lane_key) BETWEEN 1 AND 500),
    thread_id uuid,
    contact_id uuid,
    state text NOT NULL CHECK (state IN ('deciding', 'review', 'created', 'attached', 'ignored', 'applied', 'cancelled')),
    review_reason text CHECK (review_reason IN ('no_command', 'ai_unsure', 'ai_unavailable', 'too_old', 'credential', 'target_changed',
        'lane_blocked', 'requeued', 'gate_closed')),
    -- What the rules decided when it closed: what a gathering falls back on when AI does not decide.
    rules_action text NOT NULL CHECK (rules_action IN ('create', 'attach', 'review', 'archive')),
    rules_rule text NOT NULL CHECK (rules_rule ~ '^[a-z_]{1,40}$'),
    rules_target uuid,
    -- The conversation it opened or joined.
    conversation_id uuid,
    decided_by text CHECK (decided_by IN ('rules', 'ai', 'agent')),
    decided_at timestamptz,
    decided_by_agent_id uuid,
    decided_by_token_id uuid,
    -- The rules decided because AI did not in time, failed or was out of its limit.
    fallback boolean NOT NULL DEFAULT false,
    opened_at timestamptz NOT NULL,
    closed_at timestamptz NOT NULL,
    -- Until when AI may decide; past it the rules' decision applies.
    decide_by timestamptz,
    requeued_at timestamptz,
    requeued_by_agent_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, channel_id) REFERENCES channels(workspace_id, id),
    FOREIGN KEY (workspace_id, thread_id) REFERENCES channel_threads(workspace_id, id) ON DELETE SET NULL (thread_id),
    FOREIGN KEY (workspace_id, contact_id) REFERENCES contacts(workspace_id, id) ON DELETE SET NULL (contact_id),
    FOREIGN KEY (workspace_id, rules_target) REFERENCES conversations(workspace_id, id) ON DELETE SET NULL (rules_target),
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE SET NULL (conversation_id),
    FOREIGN KEY (workspace_id, decided_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (decided_by_agent_id),
    FOREIGN KEY (workspace_id, requeued_by_agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (requeued_by_agent_id),
    CHECK ((state = 'review') = (review_reason IS NOT NULL)),
    CHECK ((state = 'deciding') = (decide_by IS NOT NULL)),
    CHECK (state IN ('deciding', 'review') OR decided_by IS NOT NULL),
    CHECK (closed_at >= opened_at)
);
-- The queue: a page of 50 cards of a state, oldest first.
CREATE INDEX intake_gatherings_queue ON intake_gatherings(workspace_id, state, closed_at, id);
-- A lane's gatherings in order.
CREATE INDEX intake_gatherings_lane ON intake_gatherings(workspace_id, lane_key, closed_at);
-- What is past its time to decide, across workspaces.
CREATE INDEX intake_gatherings_due ON intake_gatherings(decide_by) WHERE state = 'deciding';
CREATE INDEX intake_gatherings_conversation ON intake_gatherings(workspace_id, conversation_id) WHERE conversation_id IS NOT NULL;
-- What was decided and what was set aside, newest first.
CREATE INDEX intake_gatherings_decided ON intake_gatherings(workspace_id, decided_at DESC, id DESC) WHERE state IN ('created', 'attached', 'applied', 'ignored');

-- Every time AI or a person decided a gathering, as it was: never changed, the labels of intake's eval.
CREATE TABLE intake_decisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    gathering_id uuid NOT NULL,
    decider text NOT NULL CHECK (decider IN ('ai', 'agent')),
    basis text NOT NULL CHECK (basis IN ('model', 'person')),
    action text NOT NULL CHECK (action IN ('create', 'attach', 'ignore', 'review')),
    target_conversation_id uuid,
    runner_up_conversation_id uuid,
    runner_up_confidence numeric(4, 3) CHECK (runner_up_confidence BETWEEN 0 AND 1),
    command text CHECK (char_length(command) BETWEEN 1 AND 40),
    subject text CHECK (char_length(subject) <= 200),
    reply_kind text CHECK (reply_kind IN ('substantive', 'acknowledgement')),
    -- A new request of a customer who had one before.
    related_conversation_id uuid,
    confidence numeric(4, 3) CHECK (confidence BETWEEN 0 AND 1),
    -- The model's reason, erased 90 days after.
    reason text CHECK (char_length(reason) <= 2000),
    applied boolean NOT NULL,
    not_applied_reason text CHECK (not_applied_reason IN ('level_suggest', 'below_threshold', 'too_old', 'credential', 'person_first',
        'target_changed', 'lane_blocked', 'gate_closed', 'limit', 'error')),
    -- The journal events it was decided on: it applies only while the gathering still has exactly these.
    items_seen uuid[] NOT NULL,
    fallback boolean NOT NULL DEFAULT false,
    -- A person who undid or changed what AI did.
    corrects_decision_id uuid,
    -- What the AI was sent, without the content (sources, counts, what was left out).
    manifest jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(manifest) = 'object' AND octet_length(manifest::text) <= 16384),
    task_id uuid,
    model text CHECK (char_length(model) <= 200),
    prompt_version text CHECK (char_length(prompt_version) <= 80),
    latency_ms integer CHECK (latency_ms >= 0),
    error_code text CHECK (error_code ~ '^[a-z_]{1,40}$'),
    agent_id uuid,
    token_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, gathering_id) REFERENCES intake_gatherings(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, target_conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE SET NULL (target_conversation_id),
    FOREIGN KEY (workspace_id, runner_up_conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE SET NULL (runner_up_conversation_id),
    FOREIGN KEY (workspace_id, related_conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE SET NULL (related_conversation_id),
    FOREIGN KEY (workspace_id, corrects_decision_id) REFERENCES intake_decisions(workspace_id, id) ON DELETE SET NULL (corrects_decision_id),
    FOREIGN KEY (workspace_id, agent_id) REFERENCES agents(workspace_id, id) ON DELETE SET NULL (agent_id),
    CHECK (applied OR not_applied_reason IS NOT NULL),
    CHECK ((decider = 'ai') = (basis = 'model'))
);
CREATE INDEX intake_decisions_gathering ON intake_decisions(workspace_id, gathering_id, created_at DESC);
CREATE INDEX intake_decisions_ai ON intake_decisions(workspace_id, created_at) WHERE decider = 'ai';

-- A journal event and a chat item belong to at most one gathering; a task of intake decides one.
ALTER TABLE channel_inbound_events ADD COLUMN gathering_id uuid,
    ADD FOREIGN KEY (workspace_id, gathering_id) REFERENCES intake_gatherings(workspace_id, id) ON DELETE SET NULL (gathering_id);
CREATE INDEX channel_inbound_events_gathering ON channel_inbound_events(workspace_id, gathering_id) WHERE gathering_id IS NOT NULL;
ALTER TABLE channel_thread_items ADD FOREIGN KEY (workspace_id, gathering_id) REFERENCES intake_gatherings(workspace_id, id) ON DELETE SET NULL (gathering_id);
CREATE INDEX channel_thread_items_gathering ON channel_thread_items(workspace_id, gathering_id, occurred_at) WHERE gathering_id IS NOT NULL;
ALTER TABLE ai_tasks ADD COLUMN gathering_id uuid,
    ADD FOREIGN KEY (workspace_id, gathering_id) REFERENCES intake_gatherings(workspace_id, id) ON DELETE CASCADE;

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
    FOREACH name IN ARRAY ARRAY['intake_gatherings', 'intake_decisions'] LOOP
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
