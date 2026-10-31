-- acme's web form suggests triage although the workspace applies it by itself, and beta's email
-- channel writes drafts by itself; beta let intake send images. A conversation of cobalt's email
-- channel started in no scope; one of beta's is marked with a scope, as a chat channel's would be.
INSERT INTO channel_ai_levels (workspace_id, channel_id, scope, slot, level, options, updated_by, updated_at) VALUES
    (md5('ws-acme')::uuid, md5('channel-acme-2')::uuid, '', 'triage', 'suggest', '{}', md5('agent-acme-admin')::uuid, '2026-10-25 12:00:00+00');
INSERT INTO channel_ai_levels (workspace_id, channel_id, scope, slot, level, options, updated_by, updated_at)
    SELECT workspace_id, id, '', 'draft', 'suggest', '{}', md5('agent-beta-admin')::uuid, '2026-10-25 12:05:00+00'
    FROM channels WHERE workspace_id = md5('ws-beta')::uuid AND type = 'email';
INSERT INTO ai_source_consents (workspace_id, source_class, consented_by, consented_at) VALUES
    (md5('ws-beta')::uuid, 'images', md5('agent-beta-admin')::uuid, '2026-10-25 12:10:00+00');
UPDATE conversations SET channel_scope = 'group'
    WHERE id = (SELECT id FROM conversations WHERE workspace_id = md5('ws-beta')::uuid AND channel_id IS NOT NULL ORDER BY created_at, id LIMIT 1);
