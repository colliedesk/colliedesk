-- When the baseline's import is UNVERIFIED for one reason the customer cannot fix, that reason: the Freshdesk
-- plan refuses the archive API while every other total was confirmed. NULL for every other case.
ALTER TABLE results_state ADD COLUMN baseline_import_cause text CHECK (baseline_import_cause IN ('archive_refused'));
