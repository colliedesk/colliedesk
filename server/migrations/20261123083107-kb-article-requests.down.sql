DROP TRIGGER kb_gap_request_conversation_deleted ON conversations;
DROP FUNCTION kb_gap_request_deleted();

-- Requests go, and each gap they were in is counted again; a gap left without signals goes too.
DO $$
DECLARE
    row record;
BEGIN
    FOR row IN
        DELETE FROM kb_gap_signals WHERE kind = 'requested' RETURNING workspace_id, gap_id
    LOOP
        PERFORM kb_gap_recount(row.workspace_id, row.gap_id);
    END LOOP;
END
$$;

ALTER TABLE kb_gap_signals DROP COLUMN note;
ALTER TABLE kb_gap_signals DROP CONSTRAINT kb_gap_signals_kind_check;
ALTER TABLE kb_gap_signals ADD CONSTRAINT kb_gap_signals_kind_check
    CHECK (kind IN ('search_no_result', 'vote_no', 'chat_handoff', 'chat_unanswered',
        'recurring_topic'));
