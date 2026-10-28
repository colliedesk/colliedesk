-- Results: the first workspace measures its baseline from a finished Freshdesk import whose numbers
-- failed once to compute; the second has no baseline and was computed an hour ago.
INSERT INTO results_state (workspace_id, baseline_source, baseline_anchor_at, baseline_import_status, baseline_dirty, computed_at, inputs, error, error_at) VALUES
    (md5('ws-acme')::uuid, 'freshdesk:account:777', now() - interval '20 days', 'OK', true, now() - interval '2 hours', '{"conversations_updated_at": "2026-10-20T08:00:00Z"}', 'statement timeout', now() - interval '2 hours'),
    (md5('ws-beta')::uuid, NULL, NULL, NULL, false, now() - interval '1 hour', '{}', NULL, NULL);
