-- An edit after history started, a restore of the first version, a reply in the trash, and an
-- edit an AI agent asked for and a person approved.
UPDATE saved_replies SET body = 'Hello {{customer.name}}, welcome back!', updated_at = '2026-10-17 12:00:00+00'
WHERE workspace_id = md5('ws-acme')::uuid AND id = md5('reply-acme-1')::uuid;
INSERT INTO saved_reply_versions (id, workspace_id, saved_reply_id, version, kind, title, body, changed_by_agent_id, created_at)
VALUES (md5('reply-acme-1-v2')::uuid, md5('ws-acme')::uuid, md5('reply-acme-1')::uuid, 2, 'edit', 'Greeting', 'Hello {{customer.name}}, welcome back!',
        md5('agent-acme-admin')::uuid, '2026-10-17 12:00:00+00');
UPDATE saved_replies SET body = 'Hello {{customer.name}}!', updated_at = '2026-10-17 12:30:00+00'
WHERE workspace_id = md5('ws-acme')::uuid AND id = md5('reply-acme-1')::uuid;
INSERT INTO saved_reply_versions (id, workspace_id, saved_reply_id, version, kind, restored_from_version, title, body, changed_by_agent_id, created_at)
VALUES (md5('reply-acme-1-v3')::uuid, md5('ws-acme')::uuid, md5('reply-acme-1')::uuid, 3, 'restore', 1, 'Greeting', 'Hello {{customer.name}}!',
        md5('agent-acme-ben')::uuid, '2026-10-17 12:30:00+00');

UPDATE saved_replies SET deleted_at = now() - interval '3 days', deleted_by_agent_id = md5('agent-beta-admin')::uuid, updated_at = now() - interval '3 days'
WHERE workspace_id = md5('ws-beta')::uuid AND id = md5('reply-beta-2')::uuid;
INSERT INTO saved_reply_versions (id, workspace_id, saved_reply_id, version, kind, title, body, changed_by_agent_id, created_at)
VALUES (md5('reply-beta-2-v2')::uuid, md5('ws-beta')::uuid, md5('reply-beta-2')::uuid, 2, 'delete', 'Password reset', 'Use the reset link on the sign-in page.',
        md5('agent-beta-admin')::uuid, now() - interval '3 days');

INSERT INTO agent_requests (id, workspace_id, token_id, token_label, token_scope, requested_by_agent_id, kind, method, route, path, body, summary, status, decided_by_agent_id, decided_at, result_status, created_at, expires_at, saved_reply_id, saved_reply_before) VALUES
    (md5('request-beta-saved-reply')::uuid, md5('ws-beta')::uuid, md5('token-beta-draft')::uuid, 'Claude (draft)', 'draft', md5('agent-beta-cy')::uuid, 'action', 'PATCH',
     '/api/v1/saved-replies/{id}', '/api/v1/saved-replies/' || md5('reply-beta-1')::uuid, '{"body": "Hi {{customer.name}}, thanks for writing."}',
     'Edit saved reply Greeting', 'approved', md5('agent-beta-admin')::uuid, '2026-10-17 13:00:00+00', 200, '2026-10-17 12:50:00+00', '2026-10-18 12:50:00+00',
     md5('reply-beta-1')::uuid, '{"title": "Greeting", "body": "Hi {{customer.name}}", "version": 1}');
UPDATE saved_replies SET body = 'Hi {{customer.name}}, thanks for writing.', updated_at = '2026-10-17 13:00:00+00'
WHERE workspace_id = md5('ws-beta')::uuid AND id = md5('reply-beta-1')::uuid;
INSERT INTO saved_reply_versions (id, workspace_id, saved_reply_id, version, kind, title, body, changed_by_agent_id, token_id, token_label, agent_request_id, created_at)
VALUES (md5('reply-beta-1-v2')::uuid, md5('ws-beta')::uuid, md5('reply-beta-1')::uuid, 2, 'edit', 'Greeting', 'Hi {{customer.name}}, thanks for writing.',
        md5('agent-beta-admin')::uuid, md5('token-beta-draft')::uuid, 'Claude (draft)', md5('request-beta-saved-reply')::uuid, '2026-10-17 13:00:00+00');
