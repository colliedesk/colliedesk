-- acme's active domain was opened at its address, and another server answered there.
UPDATE help_center_domains SET address_checked_at = '2026-10-22 08:05:01+00',
    address_error = 'https://help.acme.example answers, but not with this help center.'
WHERE workspace_id = md5('ws-acme')::uuid;
