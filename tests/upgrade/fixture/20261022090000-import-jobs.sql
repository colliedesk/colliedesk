-- acme imported its Freshdesk account in the app: the full pass is running (its recent pass ended with
-- an interim report), and an earlier import of the same account was undone in the app. The sealed key
-- is fixture bytes, not a key that opens.
INSERT INTO import_jobs (id, workspace_id, kind, source, domain, account_source, undo_of, state, phase, options, preview, progress,
        interim_report, report, report_markdown, status, error_code, error_message, sealed_key, sealed_key_id, owner_instance,
        claim_token, lease_until, next_run_at, attempts, attempts_without_progress, retrying_since, full_retries, cancel_requested_at,
        created_by, created_at, started_at, finished_at, updated_at) VALUES
    (md5('ij-acme-1')::uuid, md5('ws-acme')::uuid, 'import', 'freshdesk', 'acme.freshdesk.com', 'freshdesk:account:777', NULL, 'finished', 'full',
     '{"recent_days": 90}', '{"agents": [], "companies": 2}', '{"step": "importing tickets", "done": 4, "total": 4, "api_calls": 31}',
     '{"status": "INTERIM", "interim": true}', '{"status": "OK"}', '# Freshdesk import: OK', 'OK', NULL, NULL, NULL, NULL, NULL,
     NULL, NULL, '2026-10-22 08:00:00+00', 2, 0, NULL, 0, NULL,
     md5('agent-acme-admin')::uuid, '2026-10-22 08:00:00+00', '2026-10-22 08:01:00+00', '2026-10-22 08:30:00+00', '2026-10-22 08:30:00+00'),
    (md5('ij-acme-2')::uuid, md5('ws-acme')::uuid, 'undo', 'freshdesk', 'acme.freshdesk.com', 'freshdesk:account:777', md5('ij-acme-1')::uuid, 'failed', 'undo',
     '{}', NULL, NULL, NULL, '{"conversations": 0}', NULL, NULL, 'import_error', 'the undo stopped', NULL, NULL, NULL,
     NULL, NULL, '2026-10-22 09:00:00+00', 1, 0, NULL, 0, NULL,
     md5('agent-acme-admin')::uuid, '2026-10-22 09:00:00+00', '2026-10-22 09:00:00+00', '2026-10-22 09:01:00+00', '2026-10-22 09:01:00+00'),
    (md5('ij-acme-3')::uuid, md5('ws-acme')::uuid, 'import', 'freshdesk', 'acme.freshdesk.com', 'freshdesk:account:777', NULL, 'running', 'full',
     '{}', NULL, '{"step": "checking ticket ids", "done": 10, "total": 0, "api_calls": 50}', '{"status": "INTERIM"}', NULL, NULL, NULL,
     'freshdesk_unavailable', 'GET https://acme.freshdesk.com/api/v2/tickets: HTTP 503', '\x0102030405'::bytea, 'k1', 'fixture-instance',
     md5('ij-acme-3-claim')::uuid, '2026-10-22 10:02:00+00', '2026-10-22 10:00:00+00', 3, 1, '2026-10-22 09:30:00+00', 1, '2026-10-22 10:01:00+00',
     md5('agent-acme-admin')::uuid, '2026-10-22 09:20:00+00', '2026-10-22 09:21:00+00', NULL, '2026-10-22 10:00:00+00');
