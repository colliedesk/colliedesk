-- Sending domains registered with the mail relay: beta's is verified, cobalt's never was.
UPDATE email_channels SET provider_domain_id = 'dom_fixture_beta',
    provider_domain_records = '[{"type": "TXT", "name": "resend._domainkey.beta.example", "value": "p=fixture", "status": "verified"}]'
WHERE workspace_id = md5('ws-beta')::uuid;
UPDATE email_channels SET provider_domain_id = 'dom_fixture_cobalt',
    provider_domain_records = '[{"type": "MX", "name": "send.cobalt.example", "value": "feedback-smtp.example", "status": "pending"}]'
WHERE workspace_id = md5('ws-cobalt')::uuid;
