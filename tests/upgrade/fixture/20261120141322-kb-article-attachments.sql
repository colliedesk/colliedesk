-- acme and beta each have a text file in the library with its text read. acme's password article lists
-- it, and so does its German translation; the article's unpublished draft lists it too. beta's file
-- replaced an earlier one in place, and beta has a file upload half done.
INSERT INTO kb_media (id, workspace_id, kind, storage_key, sha256, size, content_type, counted, created_by_agent_id, created_at, last_uploaded_at,
                      name, alt, kept, updated_at, updated_by_agent_id, text)
SELECT md5('kbf-' || w.slug || '-1')::uuid, w.id, 'file', w.id::text || '/' || left(s.h, 2) || '/' || s.h, s.h,
       octet_length('colliedesk upgrade fixture kb file: ' || w.slug), 'text/plain', true, md5('agent-' || w.slug || '-' || a.agent)::uuid,
       '2026-11-20 14:00:00+00', '2026-11-20 14:00:00+00', 'Warranty form.txt', 'The warranty form', true, '2026-11-20 14:00:00+00',
       md5('agent-' || w.slug || '-' || a.agent)::uuid, 'colliedesk upgrade fixture kb file: ' || w.slug
FROM workspaces w
JOIN (VALUES ('acme', 'ben'), ('beta', 'admin')) a(slug, agent) ON a.slug = w.slug,
LATERAL (SELECT encode(sha256(convert_to('colliedesk upgrade fixture kb file: ' || w.slug, 'UTF8')), 'hex') AS h) s;

INSERT INTO kb_article_attachments (workspace_id, article_id, locale, media_id, position, created_at) VALUES
    (md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, NULL, md5('kbf-acme-1')::uuid, 0, '2026-11-20 14:01:00+00'),
    (md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, 'de', md5('kbf-acme-1')::uuid, 0, '2026-11-20 14:01:00+00'),
    (md5('ws-beta')::uuid, md5('kba-beta-1')::uuid, NULL, md5('kbf-beta-1')::uuid, 0, '2026-11-20 14:02:00+00');

UPDATE kb_article_drafts SET attachment_ids = ARRAY[md5('kbf-acme-1')::uuid] WHERE id = md5('kbd-acme-1')::uuid;

INSERT INTO kb_media_replacements (id, workspace_id, from_media_id, to_media_id, replaced_by_agent_id, created_at, in_place) VALUES
    (md5('kbmr-beta-file-1')::uuid, md5('ws-beta')::uuid, md5('kbf-beta-1')::uuid, md5('kbf-beta-0')::uuid, md5('agent-beta-admin')::uuid,
     '2026-11-20 14:03:00+00', true);

INSERT INTO kb_media_uploads (id, workspace_id, kind, name, alt, keep, size, received, created_by_agent_id, created_at, updated_at, expires_at) VALUES
    (md5('kbmu-beta-file-1')::uuid, md5('ws-beta')::uuid, 'file', 'Price list.pdf', '', true, 20000000, 8388608,
     md5('agent-beta-admin')::uuid, now() - interval '1 hour', now() - interval '50 minutes', now() + interval '23 hours');
