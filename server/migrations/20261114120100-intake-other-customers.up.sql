-- Why AI did not act on its own, also: it would set a card aside while another customer wrote in what it
-- read (their words may ask for it), so a person decides.
ALTER TABLE intake_decisions DROP CONSTRAINT intake_decisions_not_applied_reason_check,
    ADD CONSTRAINT intake_decisions_not_applied_reason_check CHECK (not_applied_reason IN ('level_suggest', 'below_threshold', 'too_old', 'credential',
        'person_first', 'target_changed', 'lane_blocked', 'gate_closed', 'limit', 'error', 'several_candidates', 'no_content', 'other_customers'));
