-- A relay domain left behind when a channel changed address, waiting to be removed.
INSERT INTO email_provider_domain_trash (workspace_id, provider_domain_id, created_at)
VALUES (md5('ws-beta')::uuid, 'dom_fixture_beta_old', '2026-10-18 07:30:00+00');
