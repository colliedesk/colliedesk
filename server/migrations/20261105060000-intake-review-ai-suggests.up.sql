-- With intake on, AI decides what a customer wrote and the rules nothing of it: at suggest every card waits
-- for a person with what AI suggests (review_reason ai_suggests); at auto what AI is not sure enough of does.
ALTER TABLE intake_gatherings DROP CONSTRAINT intake_gatherings_review_reason_check,
    ADD CONSTRAINT intake_gatherings_review_reason_check CHECK (review_reason IN ('no_command', 'ai_unsure', 'ai_unavailable', 'too_old',
        'credential', 'target_changed', 'lane_blocked', 'requeued', 'gate_closed', 'ai_suggests'));
