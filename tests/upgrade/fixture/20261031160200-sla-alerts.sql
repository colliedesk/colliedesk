-- acme's due conversation was reminded once; acme's admin keeps getting the emails.
INSERT INTO conversation_sla_alerts (workspace_id, conversation_id, clock, threshold, due_at, recipient, status, created_at)
    SELECT workspace_id, id, 'first_response', 'reminder', '2026-10-31 13:00:00+00', r.recipient, r.status, '2026-10-31 12:50:00+00'
    FROM conversations, (VALUES ('admin@acme.example', 'sent'), ('', 'done')) r(recipient, status) WHERE subject = 'Numbered in its own series';
UPDATE agents SET sla_alert_email = false WHERE id = md5('agent-acme-admin')::uuid;
UPDATE conversations SET sla_alert_since = '2026-10-31 12:00:00+00' WHERE subject = 'Numbered in its own series';
-- acme's emails came on when its targets were turned on.
UPDATE response_targets SET alerts_since = '2026-10-31 09:00:00+00';
