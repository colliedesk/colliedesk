-- acme's help center serves on its own domain, verified and checked; beta claimed a host name whose
-- DNS check has not passed yet. cobalt has none.
INSERT INTO help_center_domains (workspace_id, hostname, token, status, created_at, verified_at, checked_at, check_error, certificate_requested_at)
VALUES (md5('ws-acme')::uuid, 'help.acme.example', md5('help-domain-acme'), 'active', '2026-10-20 08:00:00+00',
        '2026-10-20 08:05:00+00', '2026-10-21 08:05:00+00', 'help.acme.example has no CNAME record pointing at acme.help.example.net yet', '2026-10-20 08:05:00+00'),
       (md5('ws-beta')::uuid, 'docs.beta.example', md5('help-domain-beta'), 'pending', '2026-10-21 09:00:00+00',
        NULL, '2026-10-21 09:01:00+00', 'docs.beta.example has no CNAME record pointing at beta.help.example.net yet', NULL);
