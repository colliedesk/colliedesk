-- cobalt's sending is paused after a complaint, and was resumed once before; a beta reply held while paused.
UPDATE workspaces SET email_paused_at = '2026-10-18 08:00:00+00', email_pause_reason = 'complaint', email_pause_resumed_at = '2026-10-17 08:00:00+00'
WHERE id = md5('ws-cobalt')::uuid;
UPDATE email_outbox SET held_by_pause = true WHERE workspace_id = md5('ws-beta')::uuid AND id = md5('outbox-beta-25')::uuid;
INSERT INTO relay_webhook_events (workspace_id, webhook_id, event_type, message_id, received_at)
VALUES (md5('ws-cobalt')::uuid, 'relay_fixture_complaint', 'email.complained', NULL, '2026-10-18 07:59:00+00');
