-- acme: Ben tagged an article through his KB writer token in one bulk action and undid it through
-- his role token; his bulk send for review, made in the web app, can still be undone.
INSERT INTO kb_bulk_operations (id, workspace_id, action, params, created_by_agent_id, created_by_token_id, created_at,
        undone_at, undone_by_agent_id, undone_by_token_id) VALUES
    (md5('kbbulk-acme-1')::uuid, md5('ws-acme')::uuid, 'tags_add', '{"action": "tags_add", "tags": ["setup"]}',
     md5('agent-acme-ben')::uuid, md5('token-acme-kb')::uuid, '2026-11-21 20:00:00+00', '2026-11-21 20:04:00+00',
     md5('agent-acme-ben')::uuid, md5('token-acme-role')::uuid),
    (md5('kbbulk-acme-2')::uuid, md5('ws-acme')::uuid, 'send_for_review', '{"action": "send_for_review"}',
     md5('agent-acme-ben')::uuid, NULL, '2026-11-21 20:10:00+00', NULL, NULL, NULL);

INSERT INTO kb_bulk_operation_articles (workspace_id, operation_id, article_id, ordinal, before, after_token, undo, ref) VALUES
    (md5('ws-acme')::uuid, md5('kbbulk-acme-1')::uuid, md5('kba-acme-1')::uuid, 0, '{"tags": ["start"]}',
     '2026-11-21 20:00:00+00', 'update', NULL),
    (md5('ws-acme')::uuid, md5('kbbulk-acme-2')::uuid, md5('kba-acme-1')::uuid, 0, '{}', NULL, 'withdraw_review',
     md5('kbr-acme-2')::text);
