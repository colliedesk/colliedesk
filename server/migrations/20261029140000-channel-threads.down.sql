-- For tests only: what chat channels said is not put back by an older release.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM channel_thread_items) THEN
        RAISE EXCEPTION 'chat threads hold messages; restore the backup taken before the upgrade instead';
    END IF;
END
$$;
DROP TABLE conversation_thread_items;
DROP TABLE channel_thread_items;
DROP TABLE channel_threads;
