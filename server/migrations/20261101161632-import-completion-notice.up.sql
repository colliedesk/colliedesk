-- Existing jobs are not backfilled: notices begin with imports ending on this version.
ALTER TABLE import_jobs
  ADD COLUMN notice_state text CHECK (notice_state IN ('pending', 'sent', 'rejected')),
  ADD COLUMN notice_next_attempt_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX import_jobs_notice_due ON import_jobs (notice_next_attempt_at) WHERE notice_state='pending';
