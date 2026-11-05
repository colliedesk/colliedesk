-- A token left a reply draft with a file on acme's second conversation, and Ben brought back the
-- earlier written draft there.
INSERT INTO ai_drafts (id, workspace_id, conversation_id, requested_by_agent_id, requested_by_token_id, status, source, text, ready_at, created_at, restored_at, restored_by_agent_id) VALUES
    (md5('aid-acme-files')::uuid, md5('ws-acme')::uuid, md5('conv-acme-2')::uuid, md5('agent-acme-ben')::uuid, NULL, 'ready', 'written',
     'The invoice is attached.', now() - interval '30 minutes', now() - interval '2 hours', now() - interval '30 minutes', md5('agent-acme-ben')::uuid);
INSERT INTO ai_draft_files (id, workspace_id, draft_id, filename, content_type, size, sha256, storage_key, created_at) VALUES
    (md5('adf-acme-invoice')::uuid, md5('ws-acme')::uuid, md5('aid-acme-files')::uuid, 'invoice.pdf', 'application/pdf', 12,
     repeat('a', 64), md5('ws-acme')::uuid::text || '/aa/' || repeat('a', 64), now() - interval '2 hours');
-- cobalt's repeated send remembers what it took from a draft (the files asked for and the ones that went).
UPDATE message_idempotency_keys SET draft_selection = jsonb_build_object('draft', md5('aid-cobalt-sent')::uuid, 'pick', jsonb_build_array(md5('adf-cobalt-kept')::uuid),
    'files', jsonb_build_array(jsonb_build_object('name', 'kept.pdf', 'size', 12, 'sha256', repeat('b', 64))))
    WHERE workspace_id = md5('ws-cobalt')::uuid AND key = 'fixture-submit-cobalt-1';
