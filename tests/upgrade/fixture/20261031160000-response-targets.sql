-- acme measures replies over weekday office hours.
INSERT INTO response_targets (workspace_id, enabled, timezone, hours, holidays, targets, warn_before_minutes, channel_ids, alert_email, enabled_at, updated_at, updated_by_agent_id)
    VALUES (md5('ws-acme')::uuid, true, 'Europe/Berlin',
        '[{"day":"mon","open":true,"from":"08:00","to":"17:00"},{"day":"tue","open":true,"from":"08:00","to":"17:00"}]',
        '[{"date":"2026-12-25","name":"Closed"}]', '{"urgent":{"first_response_minutes":240,"resolution_minutes":540}}', 60,
        ARRAY[md5('channel-acme-zalo')::uuid], true, '2026-10-31 12:00:00+00', '2026-10-31 12:00:00+00', md5('agent-acme-admin')::uuid);
