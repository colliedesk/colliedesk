-- acme took over a series from the system it left: codes from 170 on are this server's.
INSERT INTO conversation_number_series (workspace_id, series, format, next, start, start_year, created_at, updated_at)
    VALUES (md5('ws-acme')::uuid, 'ref', 'REF-{yyyy}-{n:04}', 171, 170, 2026, '2026-10-31 12:00:00+00', '2026-10-31 12:00:00+00');
INSERT INTO conversations (workspace_id, contact_id, subject, created_at, updated_at, channel, number_series)
    SELECT workspace_id, contact_id, 'Numbered in its own series', '2026-10-31 12:00:00+00', '2026-10-31 12:00:00+00', 'agent', 'ref'
    FROM conversations WHERE workspace_id = md5('ws-acme')::uuid ORDER BY created_at LIMIT 1;
