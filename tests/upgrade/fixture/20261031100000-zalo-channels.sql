-- A Zalo OA channel of the first workspace, its team's Zalo user and a group that stands for a company;
-- one delivery's signature did not match. The ids are fixture values, not a real OA's.
INSERT INTO channels (id, workspace_id, type, name, enabled, settings, external_account, created_by_agent_id, created_at, updated_at, is_default) VALUES
    (md5('channel-acme-zalo')::uuid, md5('ws-acme')::uuid, 'zalo', 'Zalo OA', true, '{}', '1000000000000000001', md5('agent-acme-admin')::uuid,
     '2026-10-31 10:00:00+00', '2026-10-31 10:00:00+00', true);
INSERT INTO zalo_channels (workspace_id, channel_id, app_id, staff_user_ids, groups, signature_failed_at, created_at, updated_at) VALUES
    (md5('ws-acme')::uuid, md5('channel-acme-zalo')::uuid, '2000000000000000002', ARRAY['3000000000000000003'], '{"group-fixture-1":"Acme Tea Co"}',
     '2026-10-31 10:30:00+00', '2026-10-31 10:00:00+00', '2026-10-31 10:05:00+00');
