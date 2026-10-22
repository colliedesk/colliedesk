-- acme ran an automatic triage that finished and a draft an agent asked for that failed on a
-- rate limit; a triage of beta was cancelled by the automatic limit. The finished triage's usage row
-- names its task and its cost on the server's provider.
INSERT INTO ai_tasks (id, workspace_id, kind, conversation_id, automatic, requested_by_agent_id, requested_by_token_id, state, attempts,
        next_run_at, claim_token, lease_until, error_code, created_at, started_at, finished_at) VALUES
    (md5('ait-acme-1')::uuid, md5('ws-acme')::uuid, 'triage', md5('conv-acme-1')::uuid, true, NULL, NULL, 'done', 1,
     '2026-10-22 13:00:00+00', NULL, NULL, NULL, '2026-10-22 13:00:00+00', '2026-10-22 13:00:01+00', '2026-10-22 13:00:04+00'),
    (md5('ait-acme-2')::uuid, md5('ws-acme')::uuid, 'draft', md5('conv-acme-1')::uuid, false, md5('agent-acme-alex')::uuid, md5('session-acme-admin')::uuid, 'running', 2,
     '2026-10-22 13:10:00+00', md5('ait-acme-2-claim')::uuid, '2026-10-22 13:13:00+00', 'rate_limited', '2026-10-22 13:05:00+00', '2026-10-22 13:05:01+00', NULL),
    (md5('ait-beta-1')::uuid, md5('ws-beta')::uuid, 'triage', md5('conv-beta-1')::uuid, true, NULL, NULL, 'cancelled', 0,
     '2026-10-22 13:20:00+00', NULL, NULL, 'automatic_limit', '2026-10-22 13:20:00+00', NULL, '2026-10-22 13:20:00+00');

UPDATE ai_usage SET task_id = md5('ait-acme-1')::uuid, automatic = true, cost_micro_usd = 42, reserved_micro_usd = 1006 WHERE id = md5('aiu-acme-1')::uuid;

INSERT INTO ai_automatic_limits (workspace_id, per_day, per_minute, updated_by, updated_at) VALUES
    (md5('ws-acme')::uuid, 500, 40, md5('agent-acme-admin')::uuid, '2026-10-22 13:30:00+00');
