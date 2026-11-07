-- beta's Billing mailbox went quiet after its email of 30 October; its admin was emailed about it.
INSERT INTO channel_quiet_spells (workspace_id, channel_id, since, detected_at) VALUES
    (md5('ws-beta')::uuid, md5('channel-beta-billing')::uuid, '2026-10-30 08:00:00+00', '2026-10-31 08:15:00+00');
INSERT INTO channel_quiet_emails (workspace_id, channel_id, since, agent_id, status, updated_at) VALUES
    (md5('ws-beta')::uuid, md5('channel-beta-billing')::uuid, '2026-10-30 08:00:00+00', md5('agent-beta-admin')::uuid, 'sent', '2026-10-31 08:15:05+00');
