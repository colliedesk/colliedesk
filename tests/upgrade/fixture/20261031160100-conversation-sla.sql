-- acme's numbered conversation was opened with its targets on, and is due.
UPDATE conversations SET sla_applies = true, sla_stale = false, sla_first_state = 'running', sla_first_due_at = '2026-10-31 13:00:00+00',
    sla_first_warn_at = '2026-10-31 12:50:00+00', sla_first_met_at = NULL, sla_resolution_state = 'running', sla_resolution_due_at = '2026-10-31 14:00:00+00',
    sla_resolution_warn_at = '2026-10-31 13:50:00+00', sla_due_at = '2026-10-31 13:00:00+00', sla_warn_at = '2026-10-31 12:50:00+00'
    WHERE subject = 'Numbered in its own series';
UPDATE conversations SET sla_first_met_at = '2026-10-31 12:30:00+00', sla_first_state = 'met', sla_applies = true
    WHERE id = (SELECT id FROM conversations WHERE workspace_id = md5('ws-acme')::uuid AND subject <> 'Numbered in its own series' ORDER BY created_at LIMIT 1);
