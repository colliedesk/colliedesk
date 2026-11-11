-- The trial-ending emails that went out: a three-day reminder and the final one to acme's admin, and a
-- three-day reminder to cobalt's admin that the relay refused for good.
INSERT INTO workspace_trial_reminders (workspace_id, agent_id, kind, trial_ends_at, status, updated_at) VALUES
    (md5('ws-acme')::uuid, md5('agent-acme-admin')::uuid, 'three_days', '2026-10-24 12:00:00+00', 'sent', '2026-10-21 12:05:00+00'),
    (md5('ws-acme')::uuid, md5('agent-acme-admin')::uuid, 'end_day', '2026-10-24 12:00:00+00', 'sent', '2026-10-24 00:05:00+00'),
    (md5('ws-cobalt')::uuid, md5('agent-cobalt-admin')::uuid, 'three_days', '2026-10-30 09:00:00+00', 'rejected', '2026-10-27 09:05:00+00');
