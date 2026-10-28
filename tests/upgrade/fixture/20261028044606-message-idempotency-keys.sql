-- cobalt's agent pressed Send on a reply and the request was repeated with the same key; the key names
-- the reply the first request stored.
INSERT INTO message_idempotency_keys (workspace_id, key, message_id, created_at)
    SELECT workspace_id, 'fixture-submit-cobalt-1', id, '2026-10-28 04:46:06+00'
    FROM messages WHERE workspace_id = md5('ws-cobalt')::uuid AND kind = 'outgoing' ORDER BY created_at, id LIMIT 1;
