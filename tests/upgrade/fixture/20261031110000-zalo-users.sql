-- Two customers of acme's Zalo OA: one named from Zalo, one Zalo had no name for.
INSERT INTO zalo_users (workspace_id, channel_id, user_id, name, looked_up_at) VALUES
    (md5('ws-acme')::uuid, md5('channel-acme-zalo')::uuid, '4000000000000000004', 'Fixture Customer', '2026-10-31 11:00:00+00'),
    (md5('ws-acme')::uuid, md5('channel-acme-zalo')::uuid, '4000000000000000005', NULL, '2026-10-31 11:01:00+00');
