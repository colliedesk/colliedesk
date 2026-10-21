-- beta has a video upload half done, and one completed into its video.
INSERT INTO kb_media_uploads (id, workspace_id, kind, name, alt, keep, size, received, created_by_agent_id, created_at, updated_at, expires_at, completed_at, media_id) VALUES
    (md5('kbmu-beta-1')::uuid, md5('ws-beta')::uuid, 'video', 'Onboarding tour', 'A tour of the inbox', true, 20000000, 8388608,
     md5('agent-beta-admin')::uuid, now() - interval '1 hour', now() - interval '50 minutes', now() + interval '23 hours', NULL, NULL),
    (md5('kbmu-beta-2')::uuid, md5('ws-beta')::uuid, 'video', 'Export an invoice', '', true, 41, 41,
     md5('agent-beta-admin')::uuid, now() - interval '2 hours', now() - interval '2 hours', now() + interval '22 hours', now() - interval '2 hours', md5('kbv-beta-1')::uuid);
UPDATE kb_media SET poster_content_type = 'image/png' WHERE id = md5('kbv-beta-1')::uuid;
