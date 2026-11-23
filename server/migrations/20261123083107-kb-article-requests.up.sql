-- A team member can ask for an article from a conversation: the request is a knowledge gap
-- signal of kind 'requested' whose source is the conversation, so similar requests and reader
-- signals share one gap and its count. A request keeps the one line its sender wrote, masked like
-- the signal text. Deleting the conversation takes its request with it. An older image never
-- writes a request and reads the new kind like any other signal text.
ALTER TABLE kb_gap_signals DROP CONSTRAINT kb_gap_signals_kind_check;
ALTER TABLE kb_gap_signals ADD CONSTRAINT kb_gap_signals_kind_check
    CHECK (kind IN ('search_no_result', 'vote_no', 'chat_handoff', 'chat_unanswered',
        'recurring_topic', 'requested'));

-- Masked: an email address or a run of 6 or more digits never reaches this column.
ALTER TABLE kb_gap_signals ADD COLUMN note text
    CHECK (note IS NULL OR char_length(note) BETWEEN 1 AND 300);

-- A deleted conversation takes its request with it, and the gap is counted again; a gap left
-- without signals goes too. While the workspace itself is being deleted its gaps go with it, so
-- the trigger writes nothing.
CREATE FUNCTION kb_gap_request_deleted() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    gap uuid;
BEGIN
    FOR gap IN
        DELETE FROM kb_gap_signals s
        WHERE s.workspace_id = OLD.workspace_id AND s.source_id = OLD.id::text
            AND s.kind = 'requested'
            AND EXISTS (SELECT 1 FROM workspaces w WHERE w.id = OLD.workspace_id)
        RETURNING s.gap_id
    LOOP
        PERFORM kb_gap_recount(OLD.workspace_id, gap);
    END LOOP;
    RETURN NULL;
END
$$;

CREATE TRIGGER kb_gap_request_conversation_deleted AFTER DELETE ON conversations
    FOR EACH ROW EXECUTE FUNCTION kb_gap_request_deleted();
