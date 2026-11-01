-- Two Freshdesk data moves acme's admin asked for (the second after a correction), and one cobalt's
-- admin asked for with no times.
INSERT INTO workspace_migration_requests (id, workspace_id, requested_by, freshdesk_address, times, requested_at) VALUES
    (md5('migration-request-acme-1')::uuid, md5('ws-acme')::uuid, md5('agent-acme-admin')::uuid, 'acme-old.freshdesk.com', 'Tuesday 10:00 to 12:00 UTC', '2026-10-25 09:00:00+00'),
    (md5('migration-request-acme-2')::uuid, md5('ws-acme')::uuid, md5('agent-acme-admin')::uuid, 'acme.freshdesk.com', 'Tuesday 10:00 to 12:00 UTC', '2026-10-25 09:30:00+00'),
    (md5('migration-request-cobalt-1')::uuid, md5('ws-cobalt')::uuid, md5('agent-cobalt-admin')::uuid, 'cobalt.freshdesk.com', '', '2026-10-30 10:00:00+00');
