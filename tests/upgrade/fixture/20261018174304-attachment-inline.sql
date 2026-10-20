-- An image shown inside a message body.
INSERT INTO attachments (id, workspace_id, message_id, filename, content_type, size, sha256, storage_key, created_at, inline)
SELECT md5('att-beta-inline')::uuid, md5('ws-beta')::uuid, md5('msg-beta-6-in')::uuid, 'inline-chart.png', 'image/png',
       octet_length(convert_to('colliedesk upgrade fixture: inline-chart.png', 'UTF8')), s, md5('ws-beta')::uuid || '/' || substr(s, 1, 2) || '/' || s,
       '2026-10-18 18:00:00+00', true
FROM (SELECT encode(sha256(convert_to('colliedesk upgrade fixture: inline-chart.png', 'UTF8')), 'hex') AS s) AS f;

-- The Help Scout importer shipped after this migration. cobalt imported one Help Scout mailbox
-- before its trial ended: 12 conversations with threads, a tag, an inline image and the ledger.
INSERT INTO contacts (id, workspace_id, name, email, organization, created_at, updated_at)
SELECT md5('contact-cobalt-hs-' || i)::uuid, md5('ws-cobalt')::uuid, 'Help Scout Customer ' || i, 'hs' || i || '@cobalt-users.example', NULL,
       timestamptz '2023-05-01 00:00:00+00' + i * interval '1 day', timestamptz '2023-05-01 00:00:00+00' + i * interval '1 day'
FROM generate_series(1, 4) AS i;
INSERT INTO tags (id, workspace_id, name, color, created_at)
VALUES (md5('tag-cobalt-helpscout')::uuid, md5('ws-cobalt')::uuid, 'from-helpscout', '#64748b', '2026-10-18 18:30:00+00');
INSERT INTO conversations (id, workspace_id, contact_id, subject, status, priority, created_at, updated_at, origin)
SELECT md5('conv-cobalt-hs-' || i)::uuid, md5('ws-cobalt')::uuid, md5('contact-cobalt-hs-' || (1 + i % 4))::uuid,
       'Help Scout conversation ' || i, CASE WHEN i % 2 = 0 THEN 'closed' ELSE 'open' END, 'normal',
       timestamptz '2023-06-01 10:00:00+00' + i * interval '9 days', timestamptz '2023-06-02 10:00:00+00' + i * interval '9 days',
       jsonb_build_object('system', 'helpscout', 'external_id', (300 + i)::text, 'type', NULL, 'source', 'email',
           'due_at', NULL, 'first_response_due_at', NULL, 'cc', '[]'::jsonb, 'forwarded_to', '[]'::jsonb)
FROM generate_series(1, 12) AS i;
INSERT INTO conversation_tags (workspace_id, conversation_id, tag_id)
SELECT md5('ws-cobalt')::uuid, md5('conv-cobalt-hs-' || i)::uuid, md5('tag-cobalt-helpscout')::uuid FROM generate_series(1, 12) AS i;
INSERT INTO messages (id, workspace_id, conversation_id, author_agent_id, author_name, kind, body, body_html, origin, created_at)
SELECT md5('msg-cobalt-hs-' || i || '-' || k)::uuid, md5('ws-cobalt')::uuid, md5('conv-cobalt-hs-' || i)::uuid, NULL,
       CASE WHEN k = 1 THEN 'Help Scout Customer ' || (1 + i % 4) ELSE 'Help Scout User 7' END,
       CASE WHEN k = 1 THEN 'incoming' ELSE 'outgoing' END,
       'Thread ' || k || ' of conversation ' || i, '<p>Thread ' || k || '</p>',
       jsonb_build_object('system', 'helpscout', 'external_id', (90000 + i * 10 + k)::text, 'kind', CASE WHEN k = 1 THEN 'customer' ELSE 'reply' END,
           'from', NULL, 'to', '[]'::jsonb, 'cc', '[]'::jsonb, 'bcc', '[]'::jsonb),
       timestamptz '2023-06-01 10:00:00+00' + i * interval '9 days' + (k - 1) * interval '4 hours'
FROM generate_series(1, 12) AS i CROSS JOIN generate_series(1, 2) AS k;
INSERT INTO attachments (id, workspace_id, message_id, filename, content_type, size, sha256, storage_key, created_at, inline)
SELECT md5('att-cobalt-hs-' || i)::uuid, md5('ws-cobalt')::uuid, md5('msg-cobalt-hs-' || i || '-1')::uuid, f, 'image/png',
       octet_length(convert_to('colliedesk upgrade fixture: ' || f, 'UTF8')), s, md5('ws-cobalt')::uuid || '/' || substr(s, 1, 2) || '/' || s,
       '2026-10-18 18:30:00+00', i = 1
FROM (SELECT i, 'hs-image-' || i || '.png' AS f, encode(sha256(convert_to('colliedesk upgrade fixture: hs-image-' || i || '.png', 'UTF8')), 'hex') AS s
      FROM generate_series(1, 3) AS i) AS files;
INSERT INTO import_items (workspace_id, source, entity, source_id, parent_source_id, target_id, status, error, meta, updated_at)
SELECT md5('ws-cobalt')::uuid, 'helpscout:mailbox:501', e.entity, e.source_id, e.parent, e.target, 'done', '', e.meta, '2026-10-18 18:30:00+00'
FROM (
    SELECT 'contact' AS entity, (600 + i)::text AS source_id, NULL AS parent, md5('contact-cobalt-hs-' || i)::uuid AS target, '{"created": true}'::jsonb AS meta
        FROM generate_series(1, 4) AS i
    UNION ALL
    SELECT 'ticket', (300 + i)::text, NULL, md5('conv-cobalt-hs-' || i)::uuid, '{"archived": false, "messages": 2}'::jsonb FROM generate_series(1, 12) AS i
    UNION ALL
    SELECT 'thread', (90000 + i * 10 + k)::text, (300 + i)::text, md5('msg-cobalt-hs-' || i || '-' || k)::uuid, '{}'::jsonb
        FROM generate_series(1, 12) AS i CROSS JOIN generate_series(1, 2) AS k
    UNION ALL
    SELECT 'attachment', (70 + i)::text, (300 + i)::text, md5('att-cobalt-hs-' || i)::uuid, '{}'::jsonb FROM generate_series(1, 3) AS i
    UNION ALL
    SELECT 'tag', 'from-helpscout', NULL, md5('tag-cobalt-helpscout')::uuid, '{"created": true}'::jsonb
) AS e;
