-- acme's due conversation was reminded once; acme's admin keeps getting the emails.
INSERT INTO conversation_sla_alerts (workspace_id, conversation_id, clock, threshold, due_at, status, recipients, created_at)
    SELECT workspace_id, id, 'first_response', 'reminder', '2026-10-31 13:00:00+00', 'sent', 1, '2026-10-31 12:50:00+00'
    FROM conversations WHERE subject = 'Numbered in its own series';
UPDATE agents SET sla_alert_email = false WHERE id = md5('agent-acme-admin')::uuid;
UPDATE conversations SET sla_alert_since = '2026-10-31 12:00:00+00' WHERE subject = 'Numbered in its own series';
