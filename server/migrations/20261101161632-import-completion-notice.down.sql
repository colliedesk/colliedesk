DROP INDEX import_jobs_notice_due;
ALTER TABLE import_jobs DROP COLUMN notice_state, DROP COLUMN notice_next_attempt_at;
