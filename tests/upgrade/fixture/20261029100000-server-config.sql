-- The configuration as a server keeps it after importing its environment and a change in the console:
-- two versions (the second restores nothing, it changes the relay), a sealed secret, a refused version,
-- one running process, the Polar products and the audit lines.
INSERT INTO server_config_versions (id, created_at, created_by, source, parent_id, note, status, trial_expires_at, request_id, rejected_reason) VALUES
  (1, '2026-10-29 10:00:00+00', 'serve v0.0.9 (env)', 'env-import', NULL, 'imported from the environment: email', 'confirmed', NULL, NULL, NULL),
  (2, '2026-10-29 10:05:00+00', 'ops@acme.example', 'console', 1, 'new relay', 'trial', '2026-10-29 10:15:00+00', md5('operator-request-1')::uuid, NULL),
  (3, '2026-10-29 10:06:00+00', 'cli:ops', 'cli', 2, '', 'rejected', NULL, NULL, 'smtp.port: SMTP_PORT must be a TCP port');
SELECT setval('server_config_versions_id_seq', 3);
INSERT INTO server_config_values (version_id, key, value, sealed, seal_key_id) VALUES
  (1, 'smtp.host', 'smtp.acme.example', NULL, NULL),
  (1, 'smtp.password', NULL, sha256('fixture-sealed-smtp-password'), 'k1'),
  (2, 'smtp.host', 'relay.acme.example', NULL, NULL),
  (2, 'smtp.password', NULL, sha256('fixture-sealed-smtp-password'), 'k1'),
  (3, 'smtp.host', 'relay.acme.example', NULL, NULL),
  (3, 'smtp.port', 'not-a-port', NULL, NULL);
UPDATE server_config_state SET desired_version = 2, last_good_version = 1, updated_at = '2026-10-29 10:05:00+00';
INSERT INTO server_processes (instance_id, hostname, binary_version, running_version, started_at, heartbeat_at, last_error, waiting_for) VALUES
  ('5f0c6a3e-fixture', 'collie-01', 'v0.0.9', 1, '2026-10-29 10:00:00+00', '2026-10-29 10:06:00+00', 'v3 refused: smtp.port', 'import of acme');
INSERT INTO polar_products (product_id, plan, period, on_sale, created_at, created_by) VALUES
  ('prod_fixture_team_m', 'team', 'monthly', true, '2026-10-29 10:00:00+00', 'serve v0.0.9 (env)'),
  ('prod_fixture_team_old', 'team', 'yearly', false, '2026-10-29 10:00:00+00', 'serve v0.0.9 (env)');
INSERT INTO operator_audit (id, at, actor_kind, actor, action, version_id, keys, detail, ip) VALUES
  (1, '2026-10-29 10:00:00+00', 'server', 'serve v0.0.9 (env)', 'env.import', 1, '{smtp.host,smtp.password}', '{}', NULL),
  (2, '2026-10-29 10:05:00+00', 'operator', 'ops@acme.example', 'config.save', 2, '{smtp.host}', '{"mode": "trial"}', '203.0.113.7');
SELECT setval('operator_audit_id_seq', 2);
