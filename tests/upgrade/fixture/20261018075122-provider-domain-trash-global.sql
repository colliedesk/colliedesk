-- A removal attempt that failed once.
UPDATE email_provider_domain_trash SET attempts = 1, last_error = 'relay answered 503', next_attempt_at = now() + interval '1 hour'
WHERE provider_domain_id = 'dom_fixture_beta_old';
