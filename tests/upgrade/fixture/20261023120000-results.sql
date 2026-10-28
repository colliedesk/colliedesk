-- Results numbers: a current snapshot and a baseline in the first workspace with the conversations
-- they count, a current one in the second; the first workspace's lease on the worker.
INSERT INTO results_snapshots (workspace_id, id, kind, computed_at, measured_at, data) VALUES
    (md5('ws-acme')::uuid, md5('results-acme-current')::uuid, 'current', now() - interval '30 minutes', now() - interval '30 minutes',
        '{"windows": {"30d": {"first_response": {"median_seconds": 1800, "p90_seconds": 7200, "answered": 5, "waiting": 1}}}}'),
    (md5('ws-acme')::uuid, md5('results-acme-baseline')::uuid, 'baseline', now() - interval '20 days', now() - interval '20 days',
        '{"source": "freshdesk:account:777", "windows": {"30d": {"first_response": {"median_seconds": 3600, "answered": 9, "waiting": 0}}}}'),
    (md5('ws-beta')::uuid, md5('results-beta-current')::uuid, 'current', now() - interval '1 hour', now() - interval '1 hour', '{"windows": {}}');
INSERT INTO results_members (workspace_id, snapshot_id, metric, conversation_id) VALUES
    (md5('ws-acme')::uuid, md5('results-acme-current')::uuid, '30d.first_response', md5('conv-acme-1')::uuid),
    (md5('ws-acme')::uuid, md5('results-acme-baseline')::uuid, '30d.first_response', md5('conv-acme-2')::uuid),
    (md5('ws-beta')::uuid, md5('results-beta-current')::uuid, '30d.volume', md5('conv-beta-1')::uuid);
UPDATE results_state SET claimed_until = now() + interval '5 minutes' WHERE workspace_id = md5('ws-acme')::uuid;
