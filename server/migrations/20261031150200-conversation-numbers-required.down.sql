DROP INDEX conversations_number_lookup;
ALTER TABLE conversations DROP CONSTRAINT conversations_number_series_fkey, DROP CONSTRAINT conversations_number_key,
    ALTER COLUMN number_format DROP NOT NULL, ALTER COLUMN display_ref DROP NOT NULL, ALTER COLUMN number_year DROP NOT NULL,
    ALTER COLUMN number DROP NOT NULL;
