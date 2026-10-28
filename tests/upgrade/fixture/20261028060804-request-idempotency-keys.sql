-- cobalt's admin clicked Save draft on an article three times; the key remembers the answer of the
-- first request, which the repeats returned.
INSERT INTO request_idempotency_keys (workspace_id, operation, key, fingerprint, response, created_at)
VALUES (md5('ws-cobalt')::uuid, 'kb_article', 'fixture-save-cobalt-1', repeat('a', 64),
    '{"id": "00000000-0000-4000-8000-000000000001", "title": "Fixture article"}', '2026-10-28 06:08:04+00');
