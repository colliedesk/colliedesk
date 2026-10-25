-- beta's chat intake: what Minh wrote was decided by Bea into the conversation it is in (after AI was
-- unsure and Bea once put it back), a second batch waits in the queue, a third waits for AI. Values are
-- fixture data, not customers'.
INSERT INTO intake_gatherings (id, workspace_id, channel_id, scope, lane_key, thread_id, contact_id, state, review_reason, rules_action, rules_rule,
        rules_target, conversation_id, decided_by, decided_at, decided_by_agent_id, decided_by_token_id, fallback, opened_at, closed_at, decide_by,
        requeued_at, requeued_by_agent_id, created_at, updated_at)
    SELECT md5('gathering-beta-1')::uuid, e.workspace_id, e.channel_id, 'group', 'thread:' || md5('thread-beta-1')::uuid || ':chat_user:4410233',
        md5('thread-beta-1')::uuid, c.contact_id, 'attached', NULL, 'attach', 'thread_key', e.conversation_id, e.conversation_id, 'agent',
        '2026-10-25 12:06:00+00', md5('agent-beta-admin')::uuid, md5('token-beta-1')::uuid, true, '2026-10-25 12:00:00+00', '2026-10-25 12:05:00+00',
        NULL, '2026-10-25 12:04:00+00', md5('agent-beta-admin')::uuid, '2026-10-25 12:05:00+00', '2026-10-25 12:06:00+00'
    FROM channel_inbound_events e JOIN conversations c ON c.workspace_id = e.workspace_id AND c.id = e.conversation_id
    WHERE e.id = md5('chin-beta-1')::uuid;
INSERT INTO intake_gatherings (id, workspace_id, channel_id, scope, lane_key, thread_id, contact_id, state, review_reason, rules_action, rules_rule,
        opened_at, closed_at)
    SELECT md5('gathering-beta-2')::uuid, workspace_id, channel_id, 'group', lane_key, thread_id, contact_id, 'review', 'no_command', 'review', 'review_hold',
        '2026-10-25 12:10:00+00', '2026-10-25 12:15:00+00'
    FROM intake_gatherings WHERE id = md5('gathering-beta-1')::uuid;
INSERT INTO intake_gatherings (id, workspace_id, channel_id, scope, lane_key, thread_id, contact_id, state, rules_action, rules_rule, opened_at, closed_at, decide_by)
    SELECT md5('gathering-beta-3')::uuid, workspace_id, channel_id, 'group', lane_key, thread_id, contact_id, 'deciding', 'create', 'new_thread',
        '2026-10-25 12:20:00+00', '2026-10-25 12:25:00+00', '2026-10-25 12:27:00+00'
    FROM intake_gatherings WHERE id = md5('gathering-beta-1')::uuid;
INSERT INTO intake_decisions (id, workspace_id, gathering_id, decider, basis, action, target_conversation_id, runner_up_conversation_id, runner_up_confidence,
        command, subject, reply_kind, related_conversation_id, confidence, reason, applied, not_applied_reason, items_seen, fallback, manifest, task_id, model,
        prompt_version, latency_ms, error_code, created_at)
    SELECT md5('decision-beta-1')::uuid, g.workspace_id, g.id, 'ai', 'model', 'attach', g.conversation_id, g.conversation_id, 0.410, 'open', 'Pricing question',
        'substantive', g.conversation_id, 0.720, 'Asks about the same plan.', false, 'below_threshold', ARRAY[md5('chin-beta-1')::uuid], true,
        '{"sources": ["thread_window"], "images_sent": 0}', md5('task-beta-intake')::uuid, 'claude-haiku-5-5', 'intake-2026-10-25.1', 840, 'timeout',
        '2026-10-25 12:05:30+00'
    FROM intake_gatherings g WHERE g.id = md5('gathering-beta-1')::uuid;
INSERT INTO intake_decisions (id, workspace_id, gathering_id, decider, basis, action, target_conversation_id, applied, items_seen, corrects_decision_id,
        agent_id, token_id, created_at)
    SELECT md5('decision-beta-2')::uuid, g.workspace_id, g.id, 'agent', 'person', 'attach', g.conversation_id, true, ARRAY[md5('chin-beta-1')::uuid],
        md5('decision-beta-1')::uuid, md5('agent-beta-admin')::uuid, md5('token-beta-1')::uuid, '2026-10-25 12:06:00+00'
    FROM intake_gatherings g WHERE g.id = md5('gathering-beta-1')::uuid;
UPDATE channel_inbound_events SET gathering_id = md5('gathering-beta-1')::uuid WHERE id = md5('chin-beta-1')::uuid;
UPDATE channel_thread_items SET gathering_id = md5('gathering-beta-1')::uuid WHERE id = md5('item-beta-1')::uuid;
INSERT INTO ai_tasks (id, workspace_id, kind, automatic, state, gathering_id, created_at)
    SELECT md5('task-beta-intake')::uuid, workspace_id, 'intake', true, 'queued', id, '2026-10-25 12:25:00+00'
    FROM intake_gatherings WHERE id = md5('gathering-beta-3')::uuid;
