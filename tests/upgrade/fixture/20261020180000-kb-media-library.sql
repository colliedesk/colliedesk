-- The media library: acme's image gets a name, alt text, size, thumbnail and the kept mark; beta's is
-- in the trash, edited and moved there by its admin; beta gets a video with a poster. A thumbnail file
-- holds 'colliedesk upgrade fixture kb thumbnail: ' || the workspace slug, a video
-- 'colliedesk upgrade fixture kb video: ' || slug and its poster 'colliedesk upgrade fixture kb poster: ' || slug.
UPDATE kb_media m SET name = 'Settings screen', alt = 'The Settings page with Password open', width = 1280, height = 720,
       thumb_storage_key = m.workspace_id || '/' || left(h, 2) || '/' || h, thumb_size = octet_length('colliedesk upgrade fixture kb thumbnail: ' || w.slug),
       inspected_at = '2026-10-20 18:00:00+00', kept = w.slug = 'acme', updated_at = '2026-10-20 18:00:00+00', updated_by_agent_id = md5('agent-' || w.slug || '-admin')::uuid,
       deleted_at = CASE WHEN w.slug = 'beta' THEN now() - interval '1 day' END,
       deleted_by_agent_id = CASE WHEN w.slug = 'beta' THEN md5('agent-beta-admin')::uuid END
FROM workspaces w, LATERAL (SELECT encode(sha256(convert_to('colliedesk upgrade fixture kb thumbnail: ' || w.slug, 'UTF8')), 'hex') AS h) s
WHERE w.id = m.workspace_id;

INSERT INTO kb_media (id, workspace_id, kind, storage_key, sha256, size, content_type, counted, created_by_agent_id, created_at, last_uploaded_at,
                      name, alt, width, height, duration_ms, codecs, thumb_storage_key, thumb_size, poster_storage_key, poster_size, inspected_at,
                      kept, updated_at, updated_by_agent_id)
SELECT md5('kbv-beta-1')::uuid, md5('ws-beta')::uuid, 'video', md5('ws-beta')::uuid || '/' || left(v, 2) || '/' || v, v,
       octet_length('colliedesk upgrade fixture kb video: beta'), 'video/mp4', true, md5('agent-beta-admin')::uuid, '2026-10-20 18:00:00+00', '2026-10-20 18:00:00+00',
       'Export an invoice', 'Exporting an invoice from Billing', 1920, 1080, 60000, '{avc1.64001f,mp4a.40.2}',
       md5('ws-beta')::uuid || '/' || left(t, 2) || '/' || t, octet_length('colliedesk upgrade fixture kb video thumbnail: beta'),
       md5('ws-beta')::uuid || '/' || left(p, 2) || '/' || p, octet_length('colliedesk upgrade fixture kb poster: beta'), '2026-10-20 18:00:00+00',
       true, '2026-10-20 18:00:00+00', md5('agent-beta-admin')::uuid
FROM (SELECT encode(sha256(convert_to('colliedesk upgrade fixture kb video: beta', 'UTF8')), 'hex') AS v,
             encode(sha256(convert_to('colliedesk upgrade fixture kb video thumbnail: beta', 'UTF8')), 'hex') AS t,
             encode(sha256(convert_to('colliedesk upgrade fixture kb poster: beta', 'UTF8')), 'hex') AS p) s;

INSERT INTO kb_article_media (workspace_id, article_id, media_id, current) VALUES
    (md5('ws-beta')::uuid, md5('kba-beta-1')::uuid, md5('kbv-beta-1')::uuid, true);
UPDATE kb_articles SET body_md = body_md || E'\n\n<video controls src="/api/v1/kb/media/' || md5('kbv-beta-1')::uuid || '"></video>'
WHERE workspace_id = md5('ws-beta')::uuid AND id = md5('kba-beta-1')::uuid;
