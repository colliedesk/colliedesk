-- "Was this article helpful?": acme's first article has a reset made by an agent through a token,
-- a vote it closed, a reader's current "No" with its masked line and another reader's "Yes" from the
-- same address; beta's invoice article has one current "No". Each vote and reset bumps its article's
-- votes version.
INSERT INTO kb_article_vote_resets (id, workspace_id, article_id, helpful, not_helpful, reset_by_agent_id, token_id, token_label, created_at) VALUES
    (md5('kbvr-acme-1')::uuid, md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, 12, 3, md5('agent-acme-ben')::uuid, md5('token-acme-kb')::uuid, 'Claude (kb)',
     '2026-11-19 10:00:00+00');

INSERT INTO kb_article_votes (id, workspace_id, article_id, voter, address, helpful, missing, reset_id, created_at, updated_at) VALUES
    (md5('kbv-acme-1')::uuid, md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, sha256('reader-acme-1'::bytea), sha256('address-acme-1'::bytea), false,
     'The old menu, write to [email removed]', md5('kbvr-acme-1')::uuid, '2026-11-18 09:00:00+00', '2026-11-18 09:00:00+00'),
    (md5('kbv-acme-2')::uuid, md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, sha256('reader-acme-1'::bytea), sha256('address-acme-1'::bytea), false,
     'The screenshot shows the old menu', NULL, '2026-11-19 11:00:00+00', '2026-11-20 09:00:00+00'),
    (md5('kbv-acme-3')::uuid, md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, sha256('reader-acme-2'::bytea), sha256('address-acme-1'::bytea), true, NULL, NULL,
     '2026-11-19 12:00:00+00', '2026-11-19 12:00:00+00'),
    (md5('kbv-beta-1')::uuid, md5('ws-beta')::uuid, md5('kba-beta-1')::uuid, sha256('reader-beta-1'::bytea), sha256('address-beta-1'::bytea), false,
     'Which plan has invoices?', NULL, '2026-11-19 13:00:00+00', '2026-11-19 13:00:00+00');

INSERT INTO kb_article_vote_versions (workspace_id, article_id, version) VALUES
    (md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, 4),
    (md5('ws-beta')::uuid, md5('kba-beta-1')::uuid, 1);
