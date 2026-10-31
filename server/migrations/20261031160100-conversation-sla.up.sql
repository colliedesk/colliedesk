-- A conversation's response clocks (docs/user/response-targets.md): whether its workspace's targets apply
-- to it (decided when it is opened: the targets were on, its channel is one they cover, it is no import or
-- problem report; an import sets it itself), and the state of its first-response and resolution clocks,
-- with the times they are due and warn. The business-time arithmetic is the server's (domain.SLAPolicy):
-- what needs none is applied here at once (pending pauses, resolved or closed stops, the first reply meets
-- the first response), and the row is marked stale for the server to count again. sla_due_at and
-- sla_warn_at are the earliest of the running clocks', which the inbox filters and sorts by.
ALTER TABLE conversations ADD COLUMN sla_applies boolean,
    ADD COLUMN sla_stale boolean NOT NULL DEFAULT false,
    ADD COLUMN sla_first_state text CHECK (sla_first_state IN ('running', 'paused', 'stopped', 'met')),
    ADD COLUMN sla_first_due_at timestamptz,
    ADD COLUMN sla_first_warn_at timestamptz,
    ADD COLUMN sla_first_met_at timestamptz,
    ADD COLUMN sla_resolution_state text CHECK (sla_resolution_state IN ('running', 'paused', 'stopped', 'met')),
    ADD COLUMN sla_resolution_due_at timestamptz,
    ADD COLUMN sla_resolution_warn_at timestamptz,
    ADD COLUMN sla_due_at timestamptz,
    ADD COLUMN sla_warn_at timestamptz;
UPDATE conversations SET sla_applies = false WHERE sla_applies IS NULL;
CREATE INDEX conversations_sla_due ON conversations(workspace_id, sla_due_at, id) WHERE sla_due_at IS NOT NULL;
CREATE INDEX conversations_sla_stale ON conversations(workspace_id, id) WHERE sla_stale;

CREATE FUNCTION conversations_sla_open() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.sla_applies IS NULL THEN
        SELECT COALESCE(bool_and(t.enabled AND NEW.channel NOT IN ('imported', 'problem_report')
                AND (t.channel_ids IS NULL OR NEW.channel_id = ANY(t.channel_ids))
                AND COALESCE(NEW.created_at, now()) >= t.enabled_at), false)
            INTO NEW.sla_applies FROM response_targets t WHERE t.workspace_id = NEW.workspace_id;
    END IF;
    NEW.sla_stale := NEW.sla_applies;
    RETURN NEW;
END
$$;

CREATE FUNCTION conversations_sla_change() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NOT NEW.sla_applies OR (NEW.status IS NOT DISTINCT FROM OLD.status AND NEW.priority IS NOT DISTINCT FROM OLD.priority) THEN
        RETURN NEW;
    END IF;
    NEW.sla_stale := true;
    IF NEW.status <> 'open' THEN
        IF NEW.sla_first_state IN ('running', 'paused') THEN
            NEW.sla_first_state := CASE WHEN NEW.status = 'pending' THEN 'paused' ELSE 'stopped' END;
        END IF;
        IF NEW.sla_resolution_state IN ('running', 'paused') THEN
            NEW.sla_resolution_state := CASE WHEN NEW.status = 'pending' THEN 'paused' ELSE 'stopped' END;
        END IF;
        NEW.sla_first_due_at := NULL; NEW.sla_first_warn_at := NULL;
        NEW.sla_resolution_due_at := NULL; NEW.sla_resolution_warn_at := NULL;
        NEW.sla_due_at := NULL; NEW.sla_warn_at := NULL;
    END IF;
    RETURN NEW;
END
$$;

-- The first reply of the team (no note, no notice of the channel's) meets the first response.
CREATE FUNCTION messages_sla_reply() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    UPDATE conversations SET sla_stale = true, sla_first_met_at = NEW.created_at,
            sla_first_state = CASE WHEN sla_first_state IS NULL THEN NULL ELSE 'met' END,
            sla_first_due_at = NULL, sla_first_warn_at = NULL
        WHERE workspace_id = NEW.workspace_id AND id = NEW.conversation_id AND sla_applies AND sla_first_met_at IS NULL;
    RETURN NULL;
END
$$;

CREATE TRIGGER conversations_sla_open BEFORE INSERT ON conversations FOR EACH ROW EXECUTE FUNCTION conversations_sla_open();
CREATE TRIGGER conversations_sla_change BEFORE UPDATE OF status, priority ON conversations FOR EACH ROW EXECUTE FUNCTION conversations_sla_change();
CREATE TRIGGER messages_sla_reply AFTER INSERT ON messages FOR EACH ROW
    WHEN (NEW.kind = 'outgoing' AND NEW.notice_kind IS NULL) EXECUTE FUNCTION messages_sla_reply();
