-- AI decides intake cards (docs/internals/channels.md "Intake"): one task at a time decides a card, and a
-- person's decision says which suggestion of AI it followed or corrected (the labels of intake's eval).

-- A person who decided as AI suggested names that suggestion; one who decided otherwise names it in
-- corrects_decision_id.
ALTER TABLE intake_decisions ADD COLUMN follows_decision_id uuid,
    ADD FOREIGN KEY (workspace_id, follows_decision_id) REFERENCES intake_decisions(workspace_id, id) ON DELETE SET NULL (follows_decision_id),
    ADD CHECK (follows_decision_id IS NULL OR corrects_decision_id IS NULL);

-- One AI task decides a card at a time: the hold, the lane's turn and a person's requeue may all ask.
CREATE UNIQUE INDEX ai_tasks_intake_gathering ON ai_tasks(workspace_id, gathering_id) WHERE kind = 'intake' AND state IN ('queued', 'running');
