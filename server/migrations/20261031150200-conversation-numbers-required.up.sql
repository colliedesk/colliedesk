-- Every conversation is numbered now (20261031150100): a number, its year and code are required, and a
-- series never gives one number twice in a year.
ALTER TABLE conversations ALTER COLUMN number SET NOT NULL, ALTER COLUMN number_year SET NOT NULL, ALTER COLUMN display_ref SET NOT NULL,
    ALTER COLUMN number_format SET NOT NULL,
    ADD CONSTRAINT conversations_number_key UNIQUE (workspace_id, number_series, number_year, number),
    ADD CONSTRAINT conversations_number_series_fkey FOREIGN KEY (workspace_id, number_series)
        REFERENCES conversation_number_series (workspace_id, series);
-- A code without a year names its number in any year.
CREATE INDEX conversations_number_lookup ON conversations(workspace_id, number_series, number);
