-- AI drafts: beta changed its AI settings and has a draft a person sent with edits (the reply names
-- it) and a scheduled answer; acme has a draft still queued. Each call to the provider is metered.
INSERT INTO workspace_ai_settings (workspace_id, drafts_enabled, auto_answer_enabled, auto_answer_delay_seconds, auto_answer_min_reviewed,
        auto_answer_min_accuracy, updated_at, updated_by_agent_id) VALUES
    (md5('ws-beta')::uuid, true, true, 600, 12, 95, '2026-10-22 15:00:00+00', md5('agent-beta-admin')::uuid);

INSERT INTO ai_drafts (id, workspace_id, conversation_id, message_id, requested_by_agent_id, status, attempts, next_attempt_at, action, body,
        confidence, confidence_reasons, handoff_reason, topic_article_id, sources, triage, triage_applied_at, triage_applied_by_agent_id, model,
        error_code, outcome, similarity, decided_by_agent_id, decided_at, auto_send_at, created_at, ready_at) VALUES
    (md5('aid-beta-1')::uuid, md5('ws-beta')::uuid, md5('conv-beta-6')::uuid, md5('msg-beta-6-in')::uuid, md5('agent-beta-admin')::uuid, 'used', 2,
     '2026-10-22 15:01:00+00', 'answer', 'Annual plans are refundable within 30 days.', 'medium', '{model_unsure}', 'Check the plan first.',
     md5('kba-beta-1')::uuid, ('[{"kind":"kb_article","id":"' || md5('kba-beta-1')::uuid || '","number":1,"title":"Refunds"}]')::jsonb,
     '{"tags":["billing"],"priority":"high","reason":"Refund"}'::jsonb, '2026-10-22 15:02:00+00', md5('agent-beta-admin')::uuid, 'claude-sonnet-5-5',
     'overloaded', 'sent_edited', 0.8, md5('agent-beta-admin')::uuid, '2026-10-22 15:03:00+00', '2026-10-22 15:10:00+00',
     '2026-10-22 15:00:00+00', '2026-10-22 15:00:05+00'),
    (md5('aid-beta-2')::uuid, md5('ws-beta')::uuid, md5('conv-beta-6')::uuid, md5('msg-beta-6-in')::uuid, md5('agent-beta-admin')::uuid, 'scheduled', 1,
     '2026-10-22 15:05:00+00', 'answer', 'Refunds reach your card in 5 to 10 days.', 'high', '{}', 'None.', md5('kba-beta-1')::uuid, '[]',
     '{"tags":[],"priority":"normal","reason":"How-to"}', '2026-10-22 15:06:00+00', md5('agent-beta-admin')::uuid, 'claude-sonnet-5-5', 'none', 'auto_cancelled',
     1, md5('agent-beta-admin')::uuid, '2026-10-22 15:06:00+00', '2026-10-22 16:00:00+00', '2026-10-22 15:05:00+00', '2026-10-22 15:05:04+00');
INSERT INTO ai_drafts (id, workspace_id, conversation_id, status, created_at) VALUES
    (md5('aid-acme-1')::uuid, md5('ws-acme')::uuid, md5('conv-acme-1')::uuid, 'queued', '2026-10-22 15:00:00+00');

UPDATE messages SET ai_draft_id = md5('aid-beta-1')::uuid WHERE workspace_id = md5('ws-beta')::uuid AND id = md5('msg-beta-6-out')::uuid;

INSERT INTO ai_usage (workspace_id, ai_draft_id, feature, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, error_code, created_at) VALUES
    (md5('ws-beta')::uuid, md5('aid-beta-1')::uuid, 'draft', 'claude-sonnet-5-5', 1200, 150, 800, 400, 'overloaded', '2026-10-22 15:00:04+00'),
    (md5('ws-acme')::uuid, md5('aid-acme-1')::uuid, 'select', 'claude-haiku-5-5', 900, 20, 0, 0, 'timeout', '2026-10-22 15:00:02+00');
