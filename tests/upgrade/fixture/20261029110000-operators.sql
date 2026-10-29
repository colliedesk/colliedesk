-- Two operators: one with an authenticator and a token that asked for a change, one revoked and locked; a
-- used link and the ticket it opened, a browser session, and a repeat-proof token creation.
INSERT INTO operators (id, email, created_at, created_by, revoked_at, totp_sealed, totp_key_id, totp_enabled_at, totp_last_step, failed_codes, locked_until) VALUES
  (md5('op-ada')::uuid, 'ada@ops.example', '2026-10-29 11:00:00+00', 'cli:root', NULL, sha256('fixture-sealed-totp'), 'k1', '2026-10-29 11:01:00+00', 59339880, 0, NULL),
  (md5('op-bob')::uuid, 'bob@ops.example', '2026-10-29 11:00:00+00', 'cli:root', '2026-10-29 12:00:00+00', NULL, NULL, NULL, 0, 3, '2026-10-29 11:30:00+00');
INSERT INTO operator_links (id, operator_id, token_hash, purpose, created_at, expires_at, used_at, created_by, enroll_sealed, enroll_key_id) VALUES
  (md5('link-ada')::uuid, md5('op-ada')::uuid, sha256('fixture-link-ada'), 'sign-in', '2026-10-29 11:00:00+00', '2026-10-29 11:15:00+00', '2026-10-29 11:00:30+00', 'cli:root', NULL, NULL),
  (md5('ticket-ada')::uuid, md5('op-ada')::uuid, sha256('fixture-ticket-ada'), 'totp-ticket', '2026-10-29 11:00:30+00', '2026-10-29 11:10:30+00', '2026-10-29 11:01:00+00', 'sign-in', sha256('fixture-enroll-ada'), 'k1');
INSERT INTO operator_sessions (id, operator_id, token_hash, kind, label, permissions, created_at, last_seen_at, expires_at, reauth_at, revoked_at, created_by_session) VALUES
  (md5('session-ada')::uuid, md5('op-ada')::uuid, sha256('fixture-session-ada'), 'cookie', 'Browser', '{}', '2026-10-29 11:01:00+00', '2026-10-29 11:05:00+00', '2026-10-29 23:01:00+00', '2026-10-29 11:01:00+00', NULL, NULL),
  (md5('token-ada')::uuid, md5('op-ada')::uuid, sha256('fixture-token-ada'), 'token', 'nightly check', '{operator:read,operator_config:write}', '2026-10-29 11:02:00+00', '2026-10-29 11:03:00+00', '2026-11-28 11:02:00+00', NULL, '2026-10-29 13:00:00+00', md5('session-ada')::uuid);
INSERT INTO operator_requests (id, session_id, method, route, path, body, summary, note, needs_secrets, status, decided_by, decided_at, result_status, result, created_at, expires_at) VALUES
  (md5('opreq-1')::uuid, md5('token-ada')::uuid, 'PATCH', 'PATCH /api/v1/operator/config', '/api/v1/operator/config', '{"set": {"smtp.host": "relay.ops.example"}, "secrets": {"smtp.password": null}}',
   'Change smtp.host', 'the relay moved', '{smtp.password}', 'approved', md5('op-ada')::uuid, '2026-10-29 11:10:00+00', 200, '{"version": 2}', '2026-10-29 11:04:00+00', '2026-11-05 11:04:00+00');
INSERT INTO operator_idempotency (operator_id, operation, key, fingerprint, answer, created_at) VALUES
  (md5('op-ada')::uuid, 'operator-token', 'press-1', sha256('fixture-fingerprint'), '{"session": {"label": "nightly check"}}', '2026-10-29 11:02:00+00');
