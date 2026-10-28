-- What AI applied stays on the conversations; only the record of it and the setting go.
DROP INDEX ai_triage_auto_applied;
UPDATE ai_triage_suggestions SET outcome = CASE WHEN applied <> '{}' THEN 'partly_accepted' ELSE 'dismissed' END
    WHERE outcome IN ('auto_applied', 'undone');
ALTER TABLE ai_triage_suggestions
    DROP CONSTRAINT ai_triage_suggestions_outcome_check,
    ADD CONSTRAINT ai_triage_suggestions_outcome_check
        CHECK (outcome IN ('accepted', 'partly_accepted', 'dismissed', 'superseded', 'expired')),
    DROP COLUMN undone_by_agent_id,
    DROP COLUMN undone_at,
    DROP COLUMN kept,
    DROP COLUMN undone_parts,
    DROP COLUMN skipped,
    DROP COLUMN auto_applied,
    DROP COLUMN auto_applied_at;
ALTER TABLE ai_slots DROP COLUMN auto_parts, DROP COLUMN automatic;
