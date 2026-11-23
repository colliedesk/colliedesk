-- acme's second conversation asked for an article: an open gap "export invoices to csv" with one
-- requested signal whose source is that conversation and which keeps the line its sender wrote.
INSERT INTO kb_gaps (id, workspace_id, title, query_key, status, askers, first_seen, last_seen, created_at) VALUES
    (md5('gap-acme-3')::uuid, md5('ws-acme')::uuid, 'Export invoices to CSV', 'export invoices to csv',
        'open', 1, '2026-11-23 08:40:00+00', '2026-11-23 08:40:00+00', '2026-11-23 08:40:00+00');
INSERT INTO kb_gap_signals (id, workspace_id, gap_id, kind, source_id, text, note, asker_key, created_at) VALUES
    (md5('gap-signal-acme-4')::uuid, md5('ws-acme')::uuid, md5('gap-acme-3')::uuid, 'requested',
        md5('conv-acme-2')::uuid::text, 'Export invoices to CSV', 'The customer wants last month in one file.',
        sha256('contact:acme-2'::bytea), '2026-11-23 08:40:00+00');
