-- Searches no longer say which help center served them; the searches themselves are kept.
DROP TRIGGER kb_searches_on_primary ON kb_searches;
ALTER TABLE kb_searches DROP CONSTRAINT kb_searches_help_center_id_fkey,
    DROP COLUMN help_center_id;
