-- Dropping the column would erase who a reply was copied to, so the rollback refuses while any
-- reply has a Cc list. Export or clear them deliberately first.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM messages WHERE cc IS NOT NULL) THEN
        RAISE EXCEPTION 'message cc rollback refused: % replies have a Cc list',
            (SELECT count(*) FROM messages WHERE cc IS NOT NULL);
    END IF;
END $$;

ALTER TABLE messages DROP COLUMN IF EXISTS cc;
