-- Messages stay; only the link to the request that sent them goes.
ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_agent_request_fkey;
ALTER TABLE messages DROP COLUMN IF EXISTS agent_request_id;
-- Dropping the table would delete the record of what agents asked and who approved it, so the
-- rollback refuses while any request exists. Delete them deliberately first.
DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    IF EXISTS (SELECT 1 FROM agent_requests) THEN
        RAISE EXCEPTION 'agent requests rollback refused: % requests would be deleted',
            (SELECT count(*) FROM agent_requests);
    END IF;
END $$;
DROP TABLE IF EXISTS agent_requests;
