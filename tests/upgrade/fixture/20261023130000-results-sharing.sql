-- Results sharing: an active link and a turned-off one in the first workspace, an active one in the
-- second; one admin turned the weekly email off; the first workspace's email of this week was claimed.
INSERT INTO results_share_links (workspace_id, id, token_hash, created_by, created_at, revoked_at, revoked_by, last_viewed_at) VALUES
    (md5('ws-acme')::uuid, md5('share-acme-old')::uuid, md5('share-token-acme-old'), md5('agent-acme-ben')::uuid, now() - interval '10 days', now() - interval '3 days', md5('agent-acme-ben')::uuid, now() - interval '4 days'),
    (md5('ws-acme')::uuid, md5('share-acme-new')::uuid, md5('share-token-acme-new'), md5('agent-acme-ben')::uuid, now() - interval '3 days', NULL, NULL, now() - interval '1 hour'),
    (md5('ws-beta')::uuid, md5('share-beta')::uuid, md5('share-token-beta'), md5('agent-beta-admin')::uuid, now() - interval '1 day', NULL, NULL, NULL);
UPDATE agents SET results_email = false WHERE id = md5('agent-beta-admin')::uuid;
UPDATE results_state SET weekly_sent_week = date_trunc('week', now()) WHERE workspace_id = md5('ws-acme')::uuid;
