-- How many conversations of the account were in the workspace when an in-app import ended (#97), so
-- a stopped import says what is already there and what an undo would remove.
ALTER TABLE import_jobs ADD COLUMN imported_conversations integer;
