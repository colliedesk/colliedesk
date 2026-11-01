DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM channels WHERE type = 'zalo') THEN
        RAISE EXCEPTION 'zalo channels rollback refused: Zalo channels exist';
    END IF;
END
$$;
DROP TABLE zalo_channels;
DELETE FROM channel_types WHERE type = 'zalo';
