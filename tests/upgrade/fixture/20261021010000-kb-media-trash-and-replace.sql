-- beta's admin moved its trashed image to the trash in a batch; acme replaced its image once and undid it.
INSERT INTO kb_media_batches (id, workspace_id, action, media_ids, created_by_agent_id, created_at, updated_at) VALUES
    (md5('kbmb-beta-1')::uuid, md5('ws-beta')::uuid, 'trash', ARRAY[md5('kbi-beta-1')::uuid], md5('agent-beta-admin')::uuid, now() - interval '1 day', now() - interval '1 day');
INSERT INTO kb_media_replacements (id, workspace_id, from_media_id, to_media_id, article_ids, article_versions, translations, skipped_article_ids, replaced_by_agent_id, created_at, undone_at, undone_by_agent_id) VALUES
    (md5('kbmr-acme-1')::uuid, md5('ws-acme')::uuid, md5('kbi-acme-1')::uuid, md5('kbi-acme-2')::uuid, ARRAY[md5('kba-acme-1')::uuid], ARRAY[3],
     jsonb_build_array(jsonb_build_object('article_id', md5('kba-acme-1')::uuid, 'locale', 'de', 'before', 'Öffnen Sie **Einstellungen**.', 'after', 'Öffnen Sie **Einstellungen**.')),
     ARRAY[md5('kba-acme-1')::uuid], md5('agent-acme-admin')::uuid,
     '2026-10-21 01:00:00+00', '2026-10-21 01:05:00+00', md5('agent-acme-admin')::uuid);
