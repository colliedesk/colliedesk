-- Dropping the table would delete text the team wrote, so the rollback refuses while any saved
-- reply exists. Export or delete them deliberately first.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM saved_replies) THEN
        RAISE EXCEPTION 'saved replies rollback refused: % saved replies would be deleted',
            (SELECT count(*) FROM saved_replies);
    END IF;
END $$;

DROP TABLE IF EXISTS saved_replies;
