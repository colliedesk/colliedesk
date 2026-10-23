-- acme lets triage apply tags and priority by itself; its first suggestion was applied by AI (the tag;
-- the assignee was left to Ben, who had taken the ticket), and Ben undid the second, keeping the
-- priority he had changed since. beta has triage on without automatic apply, assignee picked for later.
UPDATE ai_slots SET automatic = true, auto_parts = ARRAY['tags', 'priority'] WHERE workspace_id = md5('ws-acme')::uuid AND slot = 'triage';
INSERT INTO ai_slots (workspace_id, slot, uses_server, provider_id, model, effort, enabled, automatic, auto_parts, consented_by, consented_at, updated_by, updated_at) VALUES
    (md5('ws-beta')::uuid, 'triage', true, NULL, 'claude-haiku-5-5', 'low', true, false, ARRAY['assignee'],
     md5('agent-beta-admin')::uuid, '2026-10-23 08:00:00+00', md5('agent-beta-admin')::uuid, '2026-10-23 08:00:00+00');
UPDATE ai_triage_suggestions SET auto_applied_at = '2026-10-22 09:00:03+00',
        auto_applied = jsonb_build_object('tag_ids', jsonb_build_array(md5('tag-acme-bug')::uuid::text), 'priority', NULL, 'assignee_agent_id', NULL),
        skipped = jsonb_build_object('assignee', jsonb_build_object('reason', 'already_assigned', 'value', md5('agent-acme-ben')::uuid::text))
    WHERE id = md5('ats-acme-1')::uuid;
UPDATE ai_triage_suggestions SET auto_applied_at = '2026-10-22 11:00:02+00',
        undone_parts = jsonb_build_object('tag_ids', jsonb_build_array(), 'priority', 'high', 'assignee_agent_id', NULL),
        kept = jsonb_build_object('priority', 'urgent'), undone_at = '2026-10-22 11:05:00+00', undone_by_agent_id = md5('agent-acme-ben')::uuid
    WHERE id = md5('ats-acme-2')::uuid;
