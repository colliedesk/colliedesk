-- An operator offered cobalt, whose trial ran out, 14 more days twice: its admin accepted the first offer;
-- the second was revoked before anyone opened it.
INSERT INTO trial_offers (id, workspace_id, token_hash, created_by, created_at, expires_at, accepted_at, accepted_by_agent_id, accepted_by, revoked_at, revoked_by) VALUES
    (md5('offer-cobalt-accepted')::uuid, md5('ws-cobalt')::uuid, decode(md5('offer-token-accepted'), 'hex'), 'operator@hclabs.example',
     '2026-09-20 12:00:00+00', '2026-10-20 12:00:00+00', '2026-09-21 09:00:00+00', md5('agent-cobalt-admin')::uuid, 'Cole Admin', NULL, NULL),
    (md5('offer-cobalt-revoked')::uuid, md5('ws-cobalt')::uuid, decode(md5('offer-token-revoked'), 'hex'), 'operator@hclabs.example',
     '2026-09-22 12:00:00+00', '2026-10-22 12:00:00+00', NULL, NULL, NULL, '2026-09-23 12:00:00+00', 'operator@hclabs.example');
