-- acme's Zalo OA was paused while it moved over, by acme's admin.
UPDATE channels SET processing_paused_at = '2026-10-31 12:00:00+00', processing_paused_by_agent_id = md5('agent-acme-admin')::uuid
    WHERE id = md5('channel-acme-zalo')::uuid;
INSERT INTO channel_events (workspace_id, channel_id, channel_type, channel_name, kind, actor_agent_id, detail, created_at)
    VALUES (md5('ws-acme')::uuid, md5('channel-acme-zalo')::uuid, 'zalo', 'Zalo OA', 'paused', md5('agent-acme-admin')::uuid, '{}', '2026-10-31 12:00:00+00');
UPDATE channel_inbound_events SET files_fetched_at = '2026-10-31 12:00:05+00' WHERE id = md5('chin-beta-1')::uuid;
