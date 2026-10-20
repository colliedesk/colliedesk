-- Delivery events the mail relay reported for sent replies.
INSERT INTO relay_webhook_events (workspace_id, webhook_id, event_type, message_id, received_at)
SELECT md5('ws-beta')::uuid, 'relay_fixture_' || i, CASE WHEN i = 3 THEN 'email.bounced' ELSE 'email.delivered' END,
       md5('msg-beta-' || i || '-out')::uuid, timestamptz '2026-10-12 08:22:00+00' + i * interval '2 hours'
FROM generate_series(1, 3) AS i;
