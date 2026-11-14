UPDATE intake_gatherings SET review_reason = 'ai_unsure' WHERE review_reason = 'ai_suggests';
ALTER TABLE intake_gatherings DROP CONSTRAINT intake_gatherings_review_reason_check,
    ADD CONSTRAINT intake_gatherings_review_reason_check CHECK (review_reason IN ('no_command', 'ai_unsure', 'ai_unavailable', 'too_old',
        'credential', 'target_changed', 'lane_blocked', 'requeued', 'gate_closed'));
