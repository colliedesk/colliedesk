UPDATE intake_decisions SET not_applied_reason = 'below_threshold' WHERE not_applied_reason = 'other_customers';
ALTER TABLE intake_decisions DROP CONSTRAINT intake_decisions_not_applied_reason_check,
    ADD CONSTRAINT intake_decisions_not_applied_reason_check CHECK (not_applied_reason IN ('level_suggest', 'below_threshold', 'too_old', 'credential',
        'person_first', 'target_changed', 'lane_blocked', 'gate_closed', 'limit', 'error', 'several_candidates', 'no_content'));
