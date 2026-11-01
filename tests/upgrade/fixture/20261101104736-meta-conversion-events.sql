-- acme came from a Meta ad: its StartTrial went out and its Subscribe failed until Meta no longer accepted
-- it. Sent and given-up events keep their outcome; the address and browser stay until the last event is
-- settled.
INSERT INTO meta_conversion_events (workspace_id, event_name, event_time, fbc, client_ip, user_agent, event_source_url,
    value, currency, attempts, next_attempt_at, sent_at, abandoned_at, last_error, created_at) VALUES
    (md5('ws-acme')::uuid, 'StartTrial', '2026-10-21 11:30:00+00', 'fb.1.1792491600000.IwAR_acme-1', '203.0.113.7',
        'Mozilla/5.0 (Macintosh) Fixture/1.0', 'https://app.example.com/sign-in', NULL, NULL, 1,
        '2026-10-21 11:30:00+00', '2026-10-21 11:30:05+00', NULL, NULL, '2026-10-21 11:30:00+00'),
    (md5('ws-acme')::uuid, 'Subscribe', '2026-10-22 09:00:00+00', 'fb.1.1792491600000.IwAR_acme-1', '203.0.113.7',
        'Mozilla/5.0 (Macintosh) Fixture/1.0', 'https://app.example.com/settings/billing', 99.00, 'USD', 9,
        '2026-10-29 08:00:00+00', NULL, '2026-10-29 08:00:00+00', 'HTTP 503: Service temporarily unavailable', '2026-10-22 09:00:00+00');
