DO $$
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    IF EXISTS (SELECT 1 FROM conversation_number_series WHERE series <> 'default') THEN
        RAISE EXCEPTION 'conversation numbers rollback refused: % series other than the default would be lost',
            (SELECT count(*) FROM conversation_number_series WHERE series <> 'default');
    END IF;
END
$$;
DROP TRIGGER conversations_number ON conversations;
ALTER TABLE conversations DROP CONSTRAINT conversations_number_series_fkey, DROP CONSTRAINT conversations_number_key,
    DROP COLUMN display_ref, DROP COLUMN number_year, DROP COLUMN number, DROP COLUMN number_series;
DROP FUNCTION conversations_number();
DROP FUNCTION conversation_display_ref(text, bigint, integer);
DROP FUNCTION conversation_ref_read(text, text);
DROP TABLE conversation_number_series;
