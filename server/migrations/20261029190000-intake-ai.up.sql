-- AI decides intake cards (docs/internals/channels.md "Intake"): one task at a time decides a card, and a
-- person's decision says which suggestion of AI it followed or corrected (the labels of intake's eval).

-- A person who decided as AI suggested names that suggestion; one who decided otherwise names it in
-- corrects_decision_id.
ALTER TABLE intake_decisions ADD COLUMN follows_decision_id uuid,
    ADD FOREIGN KEY (workspace_id, follows_decision_id) REFERENCES intake_decisions(workspace_id, id) ON DELETE SET NULL (follows_decision_id),
    ADD CHECK (follows_decision_id IS NULL OR corrects_decision_id IS NULL);

-- Why AI did not act on its own, also: it hesitated between two or more conversations of the customer
-- (attach_requires_single_candidate), or the batch had nothing it could read (no text, no picture sent).
ALTER TABLE intake_decisions DROP CONSTRAINT intake_decisions_not_applied_reason_check,
    ADD CONSTRAINT intake_decisions_not_applied_reason_check CHECK (not_applied_reason IN ('level_suggest', 'below_threshold', 'too_old', 'credential',
        'person_first', 'target_changed', 'lane_blocked', 'gate_closed', 'limit', 'error', 'several_candidates', 'no_content'));

-- One AI task decides a card at a time: the hold, the lane's turn and a person's requeue may all ask.
CREATE UNIQUE INDEX ai_tasks_intake_gathering ON ai_tasks(workspace_id, gathering_id) WHERE kind = 'intake' AND state IN ('queued', 'running');
