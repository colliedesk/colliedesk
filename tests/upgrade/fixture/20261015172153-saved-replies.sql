-- Upgrade fixture: the data of a self-hosted install on the schema of release 0.0.1 (its last
-- migration is 20261015172153-saved-replies). No customer data: every name, address and text is
-- made up. tests/upgrade-test.sh runs this right after that migration, as a superuser; later files
-- in this directory add what later migrations made possible. See docs/internals/upgrades.md.
--
-- Three workspaces:
--   acme   imported from Freshdesk, paid (founding plan), four agents (one deactivated)
--   beta   started in Colliedesk: email channel, web form, invites, sessions, a comp plan
--   cobalt locked: its trial ended 20 days ago and nothing pays for it
-- Ids are md5 of a readable key, so a failure names a row a person can find here.
-- An attachment file holds 'colliedesk upgrade fixture: ' || filename, so files with the same name in
-- one workspace share their bytes and storage key, as the importer stores them.

INSERT INTO workspaces (id, slug, name, timezone, created_at, trial_ends_at, trial_extended_at) VALUES
    (md5('ws-acme')::uuid, 'acme', 'Acme Support', 'Europe/Berlin', '2026-10-10 08:00:00+00', now() + interval '10 days', NULL),
    (md5('ws-beta')::uuid, 'beta', 'Beta Help', 'America/New_York', '2026-10-11 09:30:00+00', now() + interval '12 days', NULL),
    (md5('ws-cobalt')::uuid, 'cobalt', 'Cobalt Desk', 'UTC', '2026-09-01 12:00:00+00', now() - interval '20 days', '2026-09-10 12:00:00+00');

INSERT INTO agents (id, workspace_id, email, display_name, role, timezone_override, created_at, deactivated_at) VALUES
    (md5('agent-acme-admin')::uuid, md5('ws-acme')::uuid, 'owner@acme.example', 'Ada Owner', 'admin', NULL, '2026-10-10 08:00:00+00', NULL),
    (md5('agent-acme-ben')::uuid, md5('ws-acme')::uuid, 'ben@acme.example', 'Ben Agent', 'agent', 'Asia/Tokyo', '2026-10-10 08:05:00+00', NULL),
    (md5('agent-acme-alex')::uuid, md5('ws-acme')::uuid, 'alex@shared.example', 'Alex Both', 'agent', NULL, '2026-10-10 08:06:00+00', NULL),
    (md5('agent-acme-gone')::uuid, md5('ws-acme')::uuid, 'gone@acme.example', 'Gwen Left', 'agent', NULL, '2026-10-10 08:07:00+00', '2026-10-12 17:00:00+00'),
    (md5('agent-beta-admin')::uuid, md5('ws-beta')::uuid, 'admin@beta.example', 'Bea Admin', 'admin', NULL, '2026-10-11 09:30:00+00', NULL),
    (md5('agent-beta-alex')::uuid, md5('ws-beta')::uuid, 'alex@shared.example', 'Alex Both', 'admin', 'Europe/Lisbon', '2026-10-11 10:00:00+00', NULL),
    (md5('agent-beta-cy')::uuid, md5('ws-beta')::uuid, 'cy@beta.example', 'Cy Agent', 'agent', NULL, '2026-10-12 10:00:00+00', NULL),
    (md5('agent-cobalt-admin')::uuid, md5('ws-cobalt')::uuid, 'admin@cobalt.example', 'Cole Admin', 'admin', NULL, '2026-09-01 12:00:00+00', NULL);

-- Contacts. The same customer address exists in acme and beta, as it would for two unrelated teams.
INSERT INTO contacts (id, workspace_id, name, email, organization, created_at, updated_at)
SELECT md5('contact-acme-' || i)::uuid, md5('ws-acme')::uuid, 'Acme Customer ' || i,
       CASE WHEN i = 1 THEN 'pat@customer.example' ELSE 'customer' || i || '@acme-clients.example' END,
       CASE WHEN i % 3 = 0 THEN NULL ELSE 'Client Org ' || (i % 5) END,
       timestamptz '2024-01-01 00:00:00+00' + i * interval '3 days',
       timestamptz '2024-06-01 00:00:00+00' + i * interval '1 day'
FROM generate_series(1, 30) AS i;
INSERT INTO contacts (id, workspace_id, name, email, organization, created_at, updated_at)
SELECT md5('contact-beta-' || i)::uuid, md5('ws-beta')::uuid, 'Beta Customer ' || i,
       CASE WHEN i = 1 THEN 'pat@customer.example' ELSE 'person' || i || '@beta-users.example' END,
       NULL, timestamptz '2026-10-11 10:00:00+00' + i * interval '1 hour', timestamptz '2026-10-11 10:00:00+00' + i * interval '1 hour'
FROM generate_series(1, 15) AS i;
INSERT INTO contacts (id, workspace_id, name, email, organization, created_at, updated_at)
SELECT md5('contact-cobalt-' || i)::uuid, md5('ws-cobalt')::uuid, 'Cobalt Customer ' || i, 'c' || i || '@cobalt-users.example',
       'Cobalt Client', '2026-09-02 00:00:00+00', '2026-09-02 00:00:00+00'
FROM generate_series(1, 3) AS i;

INSERT INTO tags (id, workspace_id, name, color, created_at)
SELECT md5('tag-acme-' || n)::uuid, md5('ws-acme')::uuid, n, c, '2026-10-10 08:30:00+00'
FROM (VALUES ('billing', '#ef4444'), ('bug', '#f59e0b'), ('vip', '#8b5cf6'), ('refund', '#64748b'), ('onboarding', '#10b981'), ('Ünïcode tag', '#0ea5e9')) AS t(n, c);
INSERT INTO tags (id, workspace_id, name, color, created_at)
SELECT md5('tag-beta-' || n)::uuid, md5('ws-beta')::uuid, n, '#64748b', '2026-10-11 11:00:00+00'
FROM (VALUES ('billing'), ('question'), ('urgent')) AS t(n);

-- acme: 60 tickets imported from Freshdesk with their original 2024 timestamps.
INSERT INTO conversations (id, workspace_id, contact_id, subject, status, priority, assignee_id, created_at, updated_at, origin, held_at, assigned_at, assigned_by_agent_id)
SELECT md5('conv-acme-' || i)::uuid, md5('ws-acme')::uuid, md5('contact-acme-' || (1 + i % 30))::uuid,
       'Imported ticket #' || (1000 + i) || CASE WHEN i % 7 = 0 THEN ' — accents é, emoji 📦' ELSE '' END,
       (ARRAY['open', 'pending', 'resolved', 'closed'])[1 + i % 4],
       (ARRAY['low', 'normal', 'high', 'urgent'])[1 + i % 4],
       CASE i % 4 WHEN 0 THEN NULL WHEN 1 THEN md5('agent-acme-ben')::uuid WHEN 2 THEN md5('agent-acme-alex')::uuid ELSE md5('agent-acme-gone')::uuid END,
       timestamptz '2024-02-01 09:00:00+00' + i * interval '5 days 3 hours',
       timestamptz '2024-02-01 09:00:00+00' + i * interval '5 days 3 hours' + interval '2 days',
       jsonb_build_object('system', 'freshdesk', 'external_id', (1000 + i)::text,
           'type', CASE WHEN i % 2 = 0 THEN 'Question' END, 'source', (ARRAY['email', 'portal', 'phone', 'chat'])[1 + i % 4],
           'due_at', to_char(timestamptz '2024-02-04 09:00:00+00' + i * interval '5 days', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
           'first_response_due_at', NULL,
           'cc', CASE WHEN i % 5 = 0 THEN jsonb_build_array('cc' || i || '@acme-clients.example') ELSE '[]'::jsonb END,
           'forwarded_to', '[]'::jsonb),
       CASE WHEN i IN (5, 17, 42) THEN timestamptz '2024-03-01 00:00:00+00' + i * interval '1 day' END,
       CASE WHEN i % 4 IN (1, 2) THEN timestamptz '2024-02-01 10:00:00+00' + i * interval '5 days 3 hours' END,
       NULL
FROM generate_series(1, 60) AS i;

-- beta: 25 conversations that arrived by email or the web form, worked in Colliedesk.
INSERT INTO conversations (id, workspace_id, contact_id, subject, status, priority, assignee_id, created_at, updated_at, origin, email_reply_token, held_at, assigned_at, assigned_by_agent_id)
SELECT md5('conv-beta-' || i)::uuid, md5('ws-beta')::uuid, md5('contact-beta-' || (1 + i % 15))::uuid,
       'Question ' || i, (ARRAY['open', 'pending', 'resolved'])[1 + i % 3], 'normal',
       CASE WHEN i % 2 = 0 THEN md5('agent-beta-cy')::uuid END,
       timestamptz '2026-10-12 08:00:00+00' + i * interval '2 hours',
       timestamptz '2026-10-12 08:00:00+00' + i * interval '2 hours' + interval '30 minutes',
       NULL, 'reply-token-beta-' || i, CASE WHEN i = 3 THEN timestamptz '2026-10-13 00:00:00+00' END,
       CASE WHEN i % 2 = 0 THEN timestamptz '2026-10-12 09:00:00+00' + i * interval '2 hours' END,
       CASE WHEN i % 2 = 0 THEN md5('agent-beta-admin')::uuid END
FROM generate_series(1, 25) AS i;

INSERT INTO conversations (id, workspace_id, contact_id, subject, status, priority, created_at, updated_at)
SELECT md5('conv-cobalt-' || i)::uuid, md5('ws-cobalt')::uuid, md5('contact-cobalt-' || (1 + i % 3))::uuid,
       'Cobalt issue ' || i, 'open', 'high', '2026-09-03 00:00:00+00', '2026-09-03 00:00:00+00'
FROM generate_series(1, 5) AS i;

INSERT INTO conversation_tags (workspace_id, conversation_id, tag_id)
SELECT md5('ws-acme')::uuid, md5('conv-acme-' || i)::uuid, md5('tag-acme-' || t)::uuid
FROM generate_series(1, 60) AS i
CROSS JOIN LATERAL (SELECT (ARRAY['billing', 'bug', 'vip', 'refund', 'onboarding', 'Ünïcode tag'])[1 + (i + k) % 6] AS t FROM generate_series(0, i % 3) AS k) AS chosen;
INSERT INTO conversation_tags (workspace_id, conversation_id, tag_id)
SELECT md5('ws-beta')::uuid, md5('conv-beta-' || i)::uuid, md5('tag-beta-' || (ARRAY['billing', 'question', 'urgent'])[1 + i % 3])::uuid
FROM generate_series(1, 25) AS i;

-- acme messages as the Freshdesk importer writes them: the description, a public reply by an
-- agent (or by a deleted Freshdesk agent, kept as author_name), and a private note.
INSERT INTO messages (id, workspace_id, conversation_id, author_agent_id, author_name, kind, body, body_html, origin, created_at)
SELECT md5('msg-acme-' || i || '-' || k)::uuid, md5('ws-acme')::uuid, md5('conv-acme-' || i)::uuid,
       CASE WHEN k = 1 THEN NULL WHEN i % 4 = 3 THEN NULL ELSE md5('agent-acme-ben')::uuid END,
       CASE WHEN k = 1 THEN 'Acme Customer ' || (1 + i % 30) WHEN i % 4 = 3 THEN 'Former Freshdesk Agent' END,
       (ARRAY['incoming', 'outgoing', 'note'])[k],
       CASE k WHEN 1 THEN 'Hello, ticket ' || i || E' has a problem.\nSecond line with a tab\tand "quotes".'
              WHEN 2 THEN 'We are on it. Reply ' || i || '.'
              ELSE 'Internal note ' || i || ': check the invoice' END,
       CASE k WHEN 1 THEN '<p>Hello, ticket ' || i || ' has a problem.</p><p>Second line</p>' WHEN 2 THEN '<div>We are on it.</div>' END,
       jsonb_build_object('system', 'freshdesk', 'external_id', (CASE WHEN k = 1 THEN 1000 + i ELSE 50000 + i * 10 + k END)::text,
           'kind', (ARRAY['description', 'reply', 'note'])[k],
           'from', CASE WHEN k = 1 THEN 'customer' || (1 + i % 30) || '@acme-clients.example' END,
           'to', '[]'::jsonb, 'cc', '[]'::jsonb, 'bcc', '[]'::jsonb),
       timestamptz '2024-02-01 09:00:00+00' + i * interval '5 days 3 hours' + (k - 1) * interval '7 hours'
FROM generate_series(1, 60) AS i CROSS JOIN generate_series(1, 3) AS k;

-- beta messages: received mail, a reply sent by SMTP, and a note on every third conversation.
INSERT INTO messages (id, workspace_id, conversation_id, author_agent_id, kind, body, body_html, email_message_id, email_meta, created_at)
SELECT md5('msg-beta-' || i || '-in')::uuid, md5('ws-beta')::uuid, md5('conv-beta-' || i)::uuid, NULL, 'incoming',
       'Hi, I need help with ' || i, '<p>Hi, I need help with ' || i || '</p>', '<question-' || i || '@beta-users.example>',
       jsonb_build_object('from', 'person' || (1 + i % 15) || '@beta-users.example', 'authentication', 'pass',
           'via', CASE WHEN i % 5 = 0 THEN 'web_form' ELSE 'platform' END, 'cc', '[]'::jsonb, 'attachments', '[]'::jsonb),
       timestamptz '2026-10-12 08:00:00+00' + i * interval '2 hours'
FROM generate_series(1, 25) AS i;
INSERT INTO messages (id, workspace_id, conversation_id, author_agent_id, kind, body, email_message_id, created_at)
SELECT md5('msg-beta-' || i || '-out')::uuid, md5('ws-beta')::uuid, md5('conv-beta-' || i)::uuid, md5('agent-beta-cy')::uuid, 'outgoing',
       'Thanks, here is the answer for ' || i, '<reply-' || i || '@beta.example>',
       timestamptz '2026-10-12 08:20:00+00' + i * interval '2 hours'
FROM generate_series(1, 25) AS i;
INSERT INTO messages (id, workspace_id, conversation_id, author_agent_id, kind, body, created_at)
SELECT md5('msg-beta-' || i || '-note')::uuid, md5('ws-beta')::uuid, md5('conv-beta-' || i)::uuid, md5('agent-beta-admin')::uuid, 'note',
       'Note on ' || i, timestamptz '2026-10-12 08:25:00+00' + i * interval '2 hours'
FROM generate_series(1, 25, 3) AS i;
INSERT INTO messages (id, workspace_id, conversation_id, kind, body, created_at)
SELECT md5('msg-cobalt-' || i)::uuid, md5('ws-cobalt')::uuid, md5('conv-cobalt-' || i)::uuid, 'incoming', 'Cobalt message ' || i, '2026-09-03 00:00:00+00'
FROM generate_series(1, 5) AS i;

-- Attachments: logo.png on five acme tickets shares one file, as the importer stores duplicates.
INSERT INTO attachments (id, workspace_id, message_id, filename, content_type, size, sha256, storage_key, created_at)
SELECT md5('att-' || w || '-' || i)::uuid, md5('ws-' || w)::uuid, md5(msg)::uuid, f, ct,
       octet_length(convert_to('colliedesk upgrade fixture: ' || f, 'UTF8')),
       encode(sha256(convert_to('colliedesk upgrade fixture: ' || f, 'UTF8')), 'hex'),
       md5('ws-' || w)::uuid || '/' || substr(encode(sha256(convert_to('colliedesk upgrade fixture: ' || f, 'UTF8')), 'hex'), 1, 2) || '/' ||
           encode(sha256(convert_to('colliedesk upgrade fixture: ' || f, 'UTF8')), 'hex'),
       '2024-05-01 00:00:00+00'
FROM (
    SELECT 'acme' AS w, i, 'msg-acme-' || i || '-1' AS msg,
           CASE WHEN i <= 5 THEN 'logo.png' ELSE 'invoice-' || i || '.pdf' END AS f,
           CASE WHEN i <= 5 THEN 'image/png' ELSE 'application/pdf' END AS ct
    FROM generate_series(1, 15) AS i
    UNION ALL
    SELECT 'beta', i, 'msg-beta-' || i || '-in', 'screenshot ' || i || '.jpg', 'image/jpeg' FROM generate_series(1, 5) AS i
    UNION ALL
    SELECT 'cobalt', 1, 'msg-cobalt-1', 'logo.png', 'image/png'
) AS files;

INSERT INTO time_entries (id, workspace_id, conversation_id, agent_id, agent_name, minutes, note, billable, executed_at, created_at)
SELECT md5('time-acme-' || i)::uuid, md5('ws-acme')::uuid, md5('conv-acme-' || i)::uuid,
       CASE WHEN i % 2 = 0 THEN md5('agent-acme-ben')::uuid END, CASE WHEN i % 2 = 0 THEN 'Ben Agent' ELSE 'Former Freshdesk Agent' END,
       15 * i, 'Freshdesk time entry ' || i, i % 3 <> 0, timestamptz '2024-04-01 10:00:00+00' + i * interval '1 day', '2026-10-10 09:00:00+00'
FROM generate_series(1, 20) AS i;
INSERT INTO time_entries (id, workspace_id, conversation_id, agent_id, agent_name, minutes, note, billable, executed_at, created_at)
SELECT md5('time-beta-' || i)::uuid, md5('ws-beta')::uuid, md5('conv-beta-' || i)::uuid, md5('agent-beta-cy')::uuid, 'Cy Agent', 10, '', true,
       '2026-10-13 10:00:00+00', '2026-10-13 10:00:00+00'
FROM generate_series(1, 3) AS i;

-- The Freshdesk import ledger: what was imported, one failure and one ticket skipped as spam.
INSERT INTO import_items (workspace_id, source, entity, source_id, parent_source_id, target_id, status, error, source_updated_at, meta, updated_at)
SELECT md5('ws-acme')::uuid, 'freshdesk:account:424242', e.entity, e.source_id, e.parent, e.target, e.status, e.error, e.updated, e.meta, '2026-10-10 09:00:00+00'
FROM (
    SELECT 'contact' AS entity, (7000 + i)::text AS source_id, NULL AS parent, md5('contact-acme-' || i)::uuid AS target, 'done' AS status, '' AS error,
           timestamptz '2024-06-01 00:00:00+00' AS updated, '{"created": true}'::jsonb AS meta FROM generate_series(1, 30) AS i
    UNION ALL
    SELECT 'ticket', (1000 + i)::text, NULL, md5('conv-acme-' || i)::uuid, 'done', '', timestamptz '2024-06-02 00:00:00+00',
           jsonb_build_object('archived', i > 50, 'messages', 3, 'attachments', CASE WHEN i <= 15 THEN 1 ELSE 0 END,
               'attachment_bytes', 0, 'time_entries', CASE WHEN i <= 20 THEN 1 ELSE 0 END) FROM generate_series(1, 60) AS i
    UNION ALL
    SELECT 'description', (1000 + i)::text, (1000 + i)::text, md5('msg-acme-' || i || '-1')::uuid, 'done', '', NULL, '{}'::jsonb FROM generate_series(1, 60) AS i
    UNION ALL
    SELECT 'conversation', (50000 + i * 10 + k)::text, (1000 + i)::text, md5('msg-acme-' || i || '-' || k)::uuid, 'done', '', NULL, '{}'::jsonb
        FROM generate_series(1, 60) AS i CROSS JOIN generate_series(2, 3) AS k
    UNION ALL
    SELECT 'attachment', 'att-' || i, (1000 + i)::text, md5('att-acme-' || i)::uuid, 'done', '', NULL, '{}'::jsonb FROM generate_series(1, 15) AS i
    UNION ALL
    SELECT 'time_entry', (9000 + i)::text, (1000 + i)::text, md5('time-acme-' || i)::uuid, 'done', '', NULL, '{}'::jsonb FROM generate_series(1, 20) AS i
    UNION ALL
    SELECT 'tag', n, NULL, md5('tag-acme-' || n)::uuid, 'done', '', NULL, '{"created": true}'::jsonb
        FROM unnest(ARRAY['billing', 'bug', 'vip', 'refund', 'onboarding', 'Ünïcode tag']) AS n
    UNION ALL
    SELECT 'ticket', '1999', NULL, NULL, 'skipped', 'spam', timestamptz '2024-06-03 00:00:00+00', '{"archived": false, "messages": 1}'::jsonb
    UNION ALL
    SELECT 'attachment', 'att-broken', '1001', NULL, 'failed', 'download failed: HTTP 404', NULL, '{}'::jsonb
    UNION ALL
    SELECT 'saved_reply', '880001', NULL, md5('reply-acme-imported')::uuid, 'done', '', NULL, '{}'::jsonb
) AS e;

INSERT INTO saved_replies (id, workspace_id, title, body, created_by_agent_id, created_at, updated_at) VALUES
    (md5('reply-acme-imported')::uuid, md5('ws-acme')::uuid, 'Refund policy (from Freshdesk)', 'Hi {{customer.name}}, refunds take 5 days. — {{agent.name}}', NULL, '2026-10-10 09:00:00+00', '2026-10-10 09:00:00+00'),
    (md5('reply-acme-1')::uuid, md5('ws-acme')::uuid, 'Greeting', 'Hello {{customer.name}}!', md5('agent-acme-admin')::uuid, '2026-10-10 10:00:00+00', '2026-10-11 10:00:00+00'),
    (md5('reply-acme-2')::uuid, md5('ws-acme')::uuid, 'Closing', E'Thanks,\n{{agent.name}}', md5('agent-acme-ben')::uuid, '2026-10-10 10:05:00+00', '2026-10-10 10:05:00+00'),
    (md5('reply-acme-3')::uuid, md5('ws-acme')::uuid, 'By a former agent', 'Kept after Gwen left.', md5('agent-acme-gone')::uuid, '2026-10-10 10:10:00+00', '2026-10-10 10:10:00+00'),
    (md5('reply-beta-1')::uuid, md5('ws-beta')::uuid, 'Greeting', 'Hi {{customer.name}}', md5('agent-beta-admin')::uuid, '2026-10-11 12:00:00+00', '2026-10-11 12:00:00+00'),
    (md5('reply-beta-2')::uuid, md5('ws-beta')::uuid, 'Password reset', 'Use the reset link on the sign-in page.', md5('agent-beta-cy')::uuid, '2026-10-11 12:05:00+00', '2026-10-12 12:05:00+00'),
    (md5('reply-cobalt-1')::uuid, md5('ws-cobalt')::uuid, 'Greeting', 'Hello from Cobalt', md5('agent-cobalt-admin')::uuid, '2026-09-02 00:00:00+00', '2026-09-02 00:00:00+00');

-- Sign-in state. Token hashes are random bytes: nobody can sign in with them.
INSERT INTO pairing_links (id, workspace_id, agent_id, email, kind, role_ceiling, token_hash, created_by_agent_id, created_at, expires_at, used_at, revoked_at) VALUES
    (md5('link-acme-pair')::uuid, md5('ws-acme')::uuid, md5('agent-acme-admin')::uuid, 'owner@acme.example', 'pairing', 'admin', sha256('fixture-link-acme-pair'), NULL, '2026-10-10 08:00:00+00', '2026-10-10 08:15:00+00', '2026-10-10 08:01:00+00', NULL),
    (md5('link-beta-invite-used')::uuid, md5('ws-beta')::uuid, md5('agent-beta-cy')::uuid, 'cy@beta.example', 'invite', 'agent', sha256('fixture-link-beta-1'), md5('agent-beta-admin')::uuid, '2026-10-12 09:00:00+00', '2026-10-19 09:00:00+00', '2026-10-12 10:00:00+00', NULL),
    (md5('link-beta-invite-revoked')::uuid, md5('ws-beta')::uuid, NULL, 'dan@beta.example', 'invite', 'agent', sha256('fixture-link-beta-2'), md5('agent-beta-admin')::uuid, '2026-10-12 09:10:00+00', now() + interval '5 days', NULL, '2026-10-12 09:20:00+00'),
    (md5('link-beta-invite-open')::uuid, md5('ws-beta')::uuid, NULL, 'eve@beta.example', 'invite', 'admin', sha256('fixture-link-beta-3'), md5('agent-beta-alex')::uuid, now() - interval '1 day', now() + interval '6 days', NULL, NULL);

INSERT INTO sessions (id, workspace_id, agent_id, token_hash, role_ceiling, label, created_at, last_seen_at, expires_at, revoked_at) VALUES
    (md5('session-acme-admin')::uuid, md5('ws-acme')::uuid, md5('agent-acme-admin')::uuid, sha256('fixture-session-1'), 'admin', 'Firefox on Linux', '2026-10-10 08:01:00+00', now() - interval '1 hour', now() + interval '20 days', NULL),
    (md5('session-acme-ben')::uuid, md5('ws-acme')::uuid, md5('agent-acme-ben')::uuid, sha256('fixture-session-2'), 'agent', 'Safari on macOS', '2026-10-10 08:06:00+00', now() - interval '2 hours', now() + interval '20 days', NULL),
    (md5('session-beta-admin')::uuid, md5('ws-beta')::uuid, md5('agent-beta-admin')::uuid, sha256('fixture-session-3'), 'admin', 'Chrome on Windows', '2026-10-11 09:31:00+00', now() - interval '3 hours', now() + interval '20 days', NULL),
    (md5('session-beta-revoked')::uuid, md5('ws-beta')::uuid, md5('agent-beta-cy')::uuid, sha256('fixture-session-4'), 'agent', 'Old laptop', '2026-10-12 10:00:00+00', '2026-10-12 11:00:00+00', now() + interval '20 days', '2026-10-12 12:00:00+00');

INSERT INTO email_channels (workspace_id, support_address, secret_key_id, secret_hash, secret_rotated_at, domain_verification_token, domain_verified_at, created_at, updated_at) VALUES
    (md5('ws-beta')::uuid, 'support@beta.example', 'fixture-key-beta', sha256('fixture-inbound-secret'), '2026-10-11 10:00:00+00', 'fixture-verification-token', '2026-10-11 11:00:00+00', '2026-10-11 10:00:00+00', '2026-10-11 11:00:00+00');

INSERT INTO email_channels (workspace_id, support_address, domain_verification_token, created_at, updated_at) VALUES
    (md5('ws-cobalt')::uuid, 'help@cobalt.example', 'fixture-verification-token-cobalt', '2026-09-02 00:00:00+00', '2026-09-02 00:00:00+00');

-- Replies: delivered, one that failed for good, and one a server was sending when it stopped.
INSERT INTO email_outbox (id, workspace_id, message_id, status, attempts, next_attempt_at, locked_until, claim_token, last_error, sent_at, created_at, updated_at)
SELECT md5('outbox-beta-' || i)::uuid, md5('ws-beta')::uuid, md5('msg-beta-' || i || '-out')::uuid,
       CASE WHEN i = 25 THEN 'queued' WHEN i = 24 THEN 'failed' ELSE 'sent' END,
       CASE WHEN i = 24 THEN 5 WHEN i = 25 THEN 1 ELSE 1 END,
       timestamptz '2026-10-12 08:20:00+00' + i * interval '2 hours',
       CASE WHEN i = 25 THEN now() - interval '1 hour' END,
       CASE WHEN i = 25 THEN md5('claim-beta-25')::uuid END,
       CASE WHEN i = 24 THEN '550 mailbox unavailable' END,
       CASE WHEN i < 24 THEN timestamptz '2026-10-12 08:21:00+00' + i * interval '2 hours' END,
       timestamptz '2026-10-12 08:20:00+00' + i * interval '2 hours', timestamptz '2026-10-12 08:21:00+00' + i * interval '2 hours'
FROM generate_series(1, 25) AS i;

INSERT INTO web_form_channels (workspace_id, enabled, allowed_origins, created_at, updated_at) VALUES
    (md5('ws-beta')::uuid, true, ARRAY['https://beta.example', 'https://www.beta.example'], '2026-10-11 12:00:00+00', '2026-10-11 12:00:00+00');

-- Billing and plans (cloud data; a self-hosted install keeps whatever it has).
INSERT INTO workspace_billing (workspace_id, polar_subscription_id, plan, polar_customer_id, status, current_period_end, cancel_at_period_end, ended_at, source_modified_at, updated_at) VALUES
    (md5('ws-acme')::uuid, 'sub_fixture_acme', 'founding', 'cus_fixture_acme', 'active', now() + interval '25 days', false, NULL, '2026-10-10 12:00:00+00', '2026-10-10 12:00:00+00'),
    (md5('ws-cobalt')::uuid, 'sub_fixture_cobalt', 'team', 'cus_fixture_cobalt', 'canceled', '2026-09-30 00:00:00+00', true, '2026-09-30 00:00:00+00', '2026-09-30 00:00:01+00', '2026-09-30 00:00:01+00');
UPDATE instance SET last_reported_at = now() - interval '6 hours';
INSERT INTO billing_webhook_events (workspace_id, webhook_id, event_type, received_at) VALUES
    (md5('ws-acme')::uuid, 'msg_fixture_1', 'subscription.created', '2026-10-10 12:00:00+00'),
    (md5('ws-acme')::uuid, 'msg_fixture_2', 'subscription.active', '2026-10-10 12:00:05+00');
INSERT INTO workspace_comp_plans (id, workspace_id, plan, granted_by, granted_at, until, revoked_by, revoked_at, revoke_reason) VALUES
    (md5('comp-beta-old')::uuid, md5('ws-beta')::uuid, 'team', 'operator@hclabs.example', '2026-10-11 10:00:00+00', NULL, 'operator@hclabs.example', '2026-10-12 10:00:00+00', 'replaced'),
    (md5('comp-beta-now')::uuid, md5('ws-beta')::uuid, 'team', 'operator@hclabs.example', '2026-10-12 10:00:00+00', now() + interval '60 days', NULL, NULL, NULL);
INSERT INTO workspace_trial_changes (id, workspace_id, previous_ends_at, new_ends_at, changed_by, changed_at) VALUES
    (md5('trial-cobalt')::uuid, md5('ws-cobalt')::uuid, '2026-09-15 12:00:00+00', now() - interval '20 days', 'operator@hclabs.example', '2026-09-10 12:00:00+00');
UPDATE workspaces SET retention_notice_sent_at = now() - interval '2 days' WHERE id = md5('ws-cobalt')::uuid;
INSERT INTO workspace_retention_notices (workspace_id, agent_id, status, sent_at) VALUES
    (md5('ws-cobalt')::uuid, md5('agent-cobalt-admin')::uuid, 'sent', now() - interval '2 days');
INSERT INTO retention_deletions (id, deleted_workspace_id, slug, deleted_at) VALUES
    (md5('deleted-old')::uuid, md5('ws-deleted-long-ago')::uuid, 'gone-desk', '2026-10-01 00:00:00+00');
INSERT INTO cloud_signups (email_hash, signup_workspace_id, ip_hash, created_at) VALUES
    (encode(sha256('owner@acme.example'), 'hex'), md5('ws-acme')::uuid, encode(sha256('203.0.113.7'), 'hex'), '2026-10-10 08:00:00+00'),
    (encode(sha256('someone@else.example'), 'hex'), NULL, NULL, '2026-10-09 08:00:00+00');
