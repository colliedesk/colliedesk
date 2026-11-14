-- Ada signed in on Acme's customer portal. The link was spent; the session was later revoked.
INSERT INTO portal_links (id, workspace_id, email, token_hash, created_at, expires_at, used_at) VALUES
    (md5('portal-link-acme')::uuid, md5('ws-acme')::uuid, 'ada@acme.example', decode(md5('portal-link-token'), 'hex'),
     '2026-11-14 10:00:00+00', '2026-11-14 10:15:00+00', '2026-11-14 10:01:00+00');
INSERT INTO portal_sessions (id, workspace_id, email, token_hash, created_at, expires_at, revoked_at) VALUES
    (md5('portal-session-acme')::uuid, md5('ws-acme')::uuid, 'ada@acme.example', decode(md5('portal-session-token'), 'hex'),
     '2026-11-14 10:01:00+00', '2026-12-14 10:01:00+00', '2026-11-14 12:00:00+00');
UPDATE help_centers SET portal_enabled = true WHERE workspace_id = md5('ws-acme')::uuid;
