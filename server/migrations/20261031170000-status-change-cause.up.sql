-- Why a conversation's status changed, for the people reading it (docs/user/channels.md "Status notices"):
-- the team (a person or an AI agent's token), the customer writing again, the customer's reopen command,
-- the channel closing it by itself, or an import. source stays what the results page reads ('colliedesk'
-- or 'import'); cause says who within it. The writer names it with set_config('app.status_cause', ...)
-- and, for a token, set_config('app.actor_token_id', ...); a writer that names no cause gets one from
-- what it set before this migration: import, a person, or nobody (the customer writing again).
ALTER TABLE conversation_status_changes
    ADD COLUMN cause text CHECK (cause IN ('agent', 'token', 'customer', 'command', 'auto_close', 'import')),
    ADD COLUMN actor_token_id uuid,
    -- quiet: the team changed it without telling the customer (notify:false), so closing it later by
    -- itself does not tell them either.
    ADD COLUMN quiet boolean NOT NULL DEFAULT false;

-- Before this migration a change with no actor was a customer writing again: the only writer that named
-- nobody. A change by a team member deleted since lost its actor, and reads as the customer's.
UPDATE conversation_status_changes SET cause = CASE
    WHEN source = 'import' THEN 'import'
    WHEN actor_agent_id IS NOT NULL THEN 'agent'
    ELSE 'customer' END;
ALTER TABLE conversation_status_changes ALTER COLUMN cause SET NOT NULL;

-- A writer that inserts a change itself without a cause (an importer, an older image after a rollback)
-- gets the same one.
CREATE FUNCTION conversation_status_changes_cause() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.cause IS NULL THEN
        NEW.cause := CASE WHEN NEW.source = 'import' THEN 'import' WHEN NEW.actor_agent_id IS NOT NULL THEN 'agent' ELSE 'customer' END;
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER conversation_status_changes_cause BEFORE INSERT ON conversation_status_changes
    FOR EACH ROW EXECUTE FUNCTION conversation_status_changes_cause();

CREATE OR REPLACE FUNCTION conversation_status_changes_record() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    actor uuid := NULLIF(current_setting('app.actor_agent_id', true), '')::uuid;
    origin text := COALESCE(NULLIF(current_setting('app.status_source', true), ''), 'colliedesk');
BEGIN
    -- clock_timestamp(), not now(): now() is when the transaction began, so a change committed after
    -- another (a customer reply whose transaction started first, or several changes in one transaction)
    -- would sort before it.
    INSERT INTO conversation_status_changes(workspace_id, conversation_id, from_status, to_status, changed_at, actor_agent_id, source, cause, actor_token_id, quiet)
        VALUES (NEW.workspace_id, NEW.id, OLD.status, NEW.status, clock_timestamp(), actor, origin,
                COALESCE(NULLIF(current_setting('app.status_cause', true), ''),
                         CASE WHEN origin = 'import' THEN 'import' WHEN actor IS NOT NULL THEN 'agent' ELSE 'customer' END),
                NULLIF(current_setting('app.actor_token_id', true), '')::uuid,
                COALESCE(current_setting('app.status_quiet', true), '') = 'on');
    RETURN NULL;
END
$$;

-- What a waiting status change would tell customers when an AI agent's token asked for it, one per chat
-- channel (domain.StatusNoticePreview): the approval card shows it after the request was decided too.
-- Cleared with the call after the retention (PruneAgentRequests).
ALTER TABLE agent_requests ADD COLUMN notices jsonb CHECK (notices IS NULL OR octet_length(notices::text) <= 131072);

-- When a channel's auto_close_hours was turned on: only a conversation resolved after it closes by itself,
-- so turning it on never closes (and tells) a backlog. Changing the hours keeps it; NULL while off.
ALTER TABLE channels ADD COLUMN auto_close_since timestamptz;
UPDATE channels SET auto_close_since = now() WHERE (settings->>'auto_close_hours')::int > 0;

-- A bulk status change tells its customers after it commits, not inside it (docs/internals/performance.md
-- "Bulk conversation update budget"): the update marks each conversation with the status to tell, in the
-- same statement, and the server sends the notice while the conversation still has that status. Any
-- other change of the status clears the mark, so a notice never tells a status the conversation left.
ALTER TABLE conversations ADD COLUMN status_notice_due text CHECK (status_notice_due IN ('open', 'pending', 'resolved', 'closed'));
CREATE INDEX conversations_status_notice_due ON conversations(workspace_id, id) WHERE status_notice_due IS NOT NULL;

CREATE FUNCTION conversations_status_notice_clear() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status_notice_due IS NOT DISTINCT FROM OLD.status_notice_due THEN
        NEW.status_notice_due := NULL;
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER conversations_status_notice_clear BEFORE UPDATE OF status ON conversations
    FOR EACH ROW WHEN (OLD.status IS DISTINCT FROM NEW.status)
    EXECUTE FUNCTION conversations_status_notice_clear();
