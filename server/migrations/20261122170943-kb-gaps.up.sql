-- Knowledge gaps: what customers could not find in the knowledge base, grouped into gaps the team
-- ranks by how many people asked. A signal is one thing a customer could not find: a help center
-- search with no results, a "No" vote, a help chat handed to a person, a chat turn that used no
-- article or that the reader rated not helpful, and a resolved conversation no article matches.
--
-- Triggers on the source tables queue each signal in kb_gap_pending, the way retrieval queues its
-- sources; a server worker reads the source, masks and folds its text and files it under a gap.
-- Publishing an article queues it too, so the worker can mark the gaps it answers fixed.
--
-- The three tables are agent-side data about the whole knowledge base, across every help center,
-- so they use secure_workspace_table and not secure_help_center_table: the public help center
-- never reads them and the help center reader role gets no grant on them. A vote is written by the
-- reader, so its trigger function runs as the app role (SECURITY DEFINER, owned by the app role):
-- the reader fires it and the app role's workspace isolation still binds the row it queues.
--
-- An older image ignores the tables; rolling back drops them with their triggers.

CREATE TABLE kb_gap_pending (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('search_no_result', 'vote_no', 'chat_handoff',
        'chat_unanswered', 'article_published')),
    -- The search (a number), the vote, the chat session or turn, or the article.
    source_id text NOT NULL CHECK (char_length(source_id) BETWEEN 1 AND 40),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX kb_gap_pending_workspace_idx ON kb_gap_pending(workspace_id, id);

CREATE TABLE kb_gaps (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    -- The masked text of the signal that opened it.
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 1000),
    -- The folded text of the signal that opened it: the same words join it at once.
    query_key text NOT NULL,
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'fixed', 'dismissed')),
    fixed_by_article_id uuid,
    -- Set when the worker marked it fixed because a published article matched its title.
    fixed_automatically boolean NOT NULL DEFAULT false,
    -- Distinct askers over all its signals; the list ranks by those of the last 30 days.
    askers integer NOT NULL DEFAULT 0 CHECK (askers >= 0),
    first_seen timestamptz NOT NULL,
    last_seen timestamptz NOT NULL,
    -- Who changed the status last and when; no one when the worker did.
    status_changed_by_agent_id uuid,
    status_changed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (workspace_id, id),
    CHECK (fixed_by_article_id IS NULL OR status = 'fixed'),
    CHECK (NOT fixed_automatically OR status = 'fixed'),
    FOREIGN KEY (workspace_id, fixed_by_article_id)
        REFERENCES kb_articles(workspace_id, id) ON DELETE SET NULL (fixed_by_article_id),
    FOREIGN KEY (workspace_id, status_changed_by_agent_id)
        REFERENCES agents(workspace_id, id) ON DELETE SET NULL (status_changed_by_agent_id)
);
CREATE INDEX kb_gaps_query_key_idx ON kb_gaps(workspace_id, query_key);
-- Words a new signal shares with a gap find it; the worker scores the candidates.
CREATE INDEX kb_gaps_words_idx ON kb_gaps USING gin (kb_search_vector(query_key));
CREATE INDEX kb_gaps_last_seen_idx ON kb_gaps(workspace_id, last_seen DESC);

CREATE TABLE kb_gap_signals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    gap_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('search_no_result', 'vote_no', 'chat_handoff',
        'chat_unanswered', 'recurring_topic')),
    -- The search, vote, chat session, chat turn or conversation it came from.
    source_id text NOT NULL CHECK (char_length(source_id) BETWEEN 1 AND 40),
    -- Masked: an email address or a run of 6 or more digits never reaches this column.
    text text NOT NULL CHECK (char_length(text) BETWEEN 1 AND 1000),
    -- SHA-256 of who asked (the voter, the chat session or the contact): it counts distinct
    -- askers and never leaves the server.
    asker_key bytea NOT NULL CHECK (octet_length(asker_key) = 32),
    created_at timestamptz NOT NULL,
    UNIQUE (workspace_id, kind, source_id),
    FOREIGN KEY (workspace_id, gap_id) REFERENCES kb_gaps(workspace_id, id) ON DELETE CASCADE
);
CREATE INDEX kb_gap_signals_gap_idx ON kb_gap_signals(workspace_id, gap_id, created_at DESC);

SELECT secure_workspace_table('kb_gap_pending');
SELECT secure_workspace_table('kb_gaps');
SELECT secure_workspace_table('kb_gap_signals');
-- A gap goes only with its workspace; a signal only with its chat (below) or its gap.
SELECT limit_app_role('kb_gaps', 'SELECT', 'INSERT', 'UPDATE', 'DELETE');
SELECT limit_app_role('kb_gap_signals', 'SELECT', 'INSERT', 'UPDATE', 'DELETE');

-- A search that listed nothing.
CREATE FUNCTION kb_gap_search_logged() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO kb_gap_pending(workspace_id, kind, source_id)
        SELECT NEW.workspace_id, 'search_no_result', NEW.id::text
        WHERE EXISTS (SELECT 1 FROM workspaces w WHERE w.id = NEW.workspace_id);
    RETURN NULL;
END
$$;
CREATE TRIGGER kb_gap_search_logged AFTER INSERT ON kb_searches
    FOR EACH ROW WHEN (NEW.results = 0) EXECUTE FUNCTION kb_gap_search_logged();

-- A "No" vote, and a "What was missing?" line added to one. The help center reader writes votes and
-- has no grant on kb_gap_pending, so this function runs as the app role (see the end of the file).
-- It queues only inside the voting workspace's own transaction: a vote written outside one (an
-- owner fixture, a restore) queues nothing.
CREATE FUNCTION kb_gap_vote_changed() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER AS $$
BEGIN
    INSERT INTO kb_gap_pending(workspace_id, kind, source_id)
        SELECT NEW.workspace_id, 'vote_no', NEW.id::text
        WHERE NEW.workspace_id
                = (SELECT NULLIF(current_setting('app.workspace_id', true), '')::uuid)
            AND EXISTS (SELECT 1 FROM workspaces w WHERE w.id = NEW.workspace_id);
    RETURN NULL;
END
$$;
CREATE TRIGGER kb_gap_vote_changed AFTER INSERT OR UPDATE OF helpful, missing ON kb_article_votes
    FOR EACH ROW WHEN (NOT NEW.helpful AND NEW.reset_id IS NULL)
    EXECUTE FUNCTION kb_gap_vote_changed();

-- A help chat handed to a person.
CREATE FUNCTION kb_gap_chat_handed_off() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO kb_gap_pending(workspace_id, kind, source_id)
        SELECT NEW.workspace_id, 'chat_handoff', NEW.id::text
        WHERE EXISTS (SELECT 1 FROM workspaces w WHERE w.id = NEW.workspace_id);
    RETURN NULL;
END
$$;
CREATE TRIGGER kb_gap_chat_handed_off AFTER UPDATE OF status ON ai_chat_sessions
    FOR EACH ROW WHEN (NEW.status = 'handed_off' AND OLD.status <> 'handed_off')
    EXECUTE FUNCTION kb_gap_chat_handed_off();

-- A chat turn that found no article, or that the reader rated not helpful: one signal per turn.
CREATE FUNCTION kb_gap_chat_unanswered() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    turn uuid;
BEGIN
    -- A turn is its own source; a rating names its turn.
    IF TG_TABLE_NAME = 'ai_chat_turns' THEN
        turn := NEW.id;
    ELSE
        turn := NEW.turn_id;
    END IF;
    INSERT INTO kb_gap_pending(workspace_id, kind, source_id)
        SELECT NEW.workspace_id, 'chat_unanswered', turn::text
        WHERE EXISTS (SELECT 1 FROM workspaces w WHERE w.id = NEW.workspace_id);
    RETURN NULL;
END
$$;
CREATE TRIGGER kb_gap_chat_turn_unanswered AFTER INSERT OR UPDATE OF status ON ai_chat_turns
    FOR EACH ROW WHEN (NEW.status = 'no_source') EXECUTE FUNCTION kb_gap_chat_unanswered();
CREATE TRIGGER kb_gap_chat_turn_not_helpful AFTER INSERT OR UPDATE OF rating ON ai_chat_feedback
    FOR EACH ROW WHEN (NEW.rating = 'not_helpful') EXECUTE FUNCTION kb_gap_chat_unanswered();

-- A deleted chat takes its signals with it, and each gap they were in is counted again; a gap left
-- without signals goes too, since its title came from them. While the workspace itself is being
-- deleted its gaps go with it, so the trigger writes nothing.
CREATE FUNCTION kb_gap_chat_deleted() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    gap uuid;
BEGIN
    FOR gap IN
        DELETE FROM kb_gap_signals s
        WHERE s.workspace_id = OLD.workspace_id AND s.source_id = OLD.id::text
            AND s.kind = CASE TG_TABLE_NAME WHEN 'ai_chat_sessions' THEN 'chat_handoff'
                ELSE 'chat_unanswered' END
            AND EXISTS (SELECT 1 FROM workspaces w WHERE w.id = OLD.workspace_id)
        RETURNING s.gap_id
    LOOP
        PERFORM kb_gap_recount(OLD.workspace_id, gap);
    END LOOP;
    RETURN NULL;
END
$$;

-- Counts a gap again from its signals: askers, first and last seen, and the most common text as
-- its title (the newest wins a tie). A gap without signals is deleted.
CREATE FUNCTION kb_gap_recount(ws uuid, gap uuid) RETURNS void LANGUAGE sql AS $$
    DELETE FROM kb_gaps g WHERE g.workspace_id = ws AND g.id = gap
        AND NOT EXISTS (SELECT 1 FROM kb_gap_signals s
            WHERE s.workspace_id = ws AND s.gap_id = gap);
    UPDATE kb_gaps g SET askers = x.askers, first_seen = x.first_seen, last_seen = x.last_seen,
        title = (SELECT s.text FROM kb_gap_signals s WHERE s.workspace_id = ws AND s.gap_id = gap
            GROUP BY s.text ORDER BY count(*) DESC, max(s.created_at) DESC, s.text LIMIT 1)
    FROM (SELECT count(DISTINCT s.asker_key)::int AS askers, min(s.created_at) AS first_seen,
            max(s.created_at) AS last_seen
        FROM kb_gap_signals s WHERE s.workspace_id = ws AND s.gap_id = gap) x
    WHERE g.workspace_id = ws AND g.id = gap AND x.askers > 0;
$$;

CREATE TRIGGER kb_gap_chat_session_deleted AFTER DELETE ON ai_chat_sessions
    FOR EACH ROW EXECUTE FUNCTION kb_gap_chat_deleted();
CREATE TRIGGER kb_gap_chat_turn_deleted AFTER DELETE ON ai_chat_turns
    FOR EACH ROW EXECUTE FUNCTION kb_gap_chat_deleted();

-- A published article may answer open gaps: the worker compares its title with theirs.
CREATE FUNCTION kb_gap_article_published() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO kb_gap_pending(workspace_id, kind, source_id)
        SELECT NEW.workspace_id, 'article_published', NEW.id::text
        WHERE EXISTS (SELECT 1 FROM workspaces w WHERE w.id = NEW.workspace_id);
    RETURN NULL;
END
$$;
CREATE TRIGGER kb_gap_article_inserted AFTER INSERT ON kb_articles
    FOR EACH ROW WHEN (NEW.status = 'published' AND NEW.deleted_at IS NULL)
    EXECUTE FUNCTION kb_gap_article_published();
CREATE TRIGGER kb_gap_article_updated AFTER UPDATE OF status, title, deleted_at ON kb_articles
    FOR EACH ROW WHEN (NEW.status = 'published' AND NEW.deleted_at IS NULL
        AND (OLD.status, OLD.title, OLD.deleted_at) IS DISTINCT
            FROM (NEW.status, NEW.title, NEW.deleted_at))
    EXECUTE FUNCTION kb_gap_article_published();

-- The vote trigger's function belongs to the app role, with a fixed search path. A new owner needs
-- CREATE on the schema; the app role has it only for these statements. Firing a trigger checks no
-- EXECUTE privilege, so no one needs to call the function directly.
DO $$
BEGIN
    EXECUTE format('ALTER FUNCTION kb_gap_vote_changed() SET search_path = %I, pg_temp',
        current_schema());
    EXECUTE format('GRANT CREATE ON SCHEMA %I TO %I', current_schema(), app_role());
    EXECUTE format('ALTER FUNCTION kb_gap_vote_changed() OWNER TO %I', app_role());
    EXECUTE format('REVOKE CREATE ON SCHEMA %I FROM %I', current_schema(), app_role());
END
$$;
REVOKE EXECUTE ON FUNCTION kb_gap_vote_changed() FROM PUBLIC;
