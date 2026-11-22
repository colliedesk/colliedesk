-- acme sends its help center page views to GA4 with a sealed API secret, and the last send failed
-- after an earlier one went through. beta saved a self-hosted Plausible and then stopped sending.
-- The sealed secret is fixture bytes, not a secret that opens.
INSERT INTO help_center_analytics (workspace_id, provider, enabled, ga4_measurement_id, ga4_api_secret_sealed,
        ga4_api_secret_key_id, plausible_domain, plausible_base_url, last_sent_at, last_error, last_error_at,
        updated_at, updated_by_agent_id) VALUES
    (md5('ws-acme')::uuid, 'ga4', true, 'G-ACME12345', '\x20212223'::bytea, 'k1', NULL, 'https://plausible.io',
        '2026-11-21 12:04:00+00', 'GA4 answered 503', '2026-11-21 12:30:00+00', '2026-11-21 11:00:00+00',
        md5('agent-acme-admin')::uuid),
    (md5('ws-beta')::uuid, 'plausible', false, NULL, NULL, NULL, 'help.beta.example', 'https://stats.beta.example',
        NULL, NULL, NULL, '2026-11-21 11:30:00+00', NULL);
