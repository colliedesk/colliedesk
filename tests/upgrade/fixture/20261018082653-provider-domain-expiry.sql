-- cobalt never verified its sending domain, so the relay registration expired.
UPDATE email_channels SET provider_domain_expired_at = '2026-10-18 09:00:00+00'
WHERE workspace_id = md5('ws-cobalt')::uuid;
