-- How often the transaction that ends an import job failed (#605). It is counted apart from
-- attempts_without_progress, which every rerun clears by walking the account again, so an end that keeps
-- failing fails the job after a few tries instead of running it for ever.
ALTER TABLE import_jobs ADD COLUMN end_failures integer NOT NULL DEFAULT 0;
