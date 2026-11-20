-- The migration gave every workspace the three starter templates. acme wrote a template of its own,
-- edited it through an agent token, restored the first version and made it the Getting started
-- default; beta deleted its FAQ starter, which stays in the trash.
INSERT INTO kb_templates (id, workspace_id, name, description, body, created_by_agent_id, updated_by_agent_id, created_at, updated_at) VALUES
    (md5('kbtpl-acme-1')::uuid, md5('ws-acme')::uuid, 'Release note', 'What changed and who it affects.',
     E'## What changed\n\nOne sentence.\n', md5('agent-acme-admin')::uuid, md5('agent-acme-ben')::uuid, '2026-11-20 14:00:00+00', '2026-11-20 14:20:00+00');
INSERT INTO kb_template_versions (id, workspace_id, template_id, version, kind, restored_from_version, name, description, body, changed_by_agent_id, token_id, token_label, created_at) VALUES
    (md5('kbtpl-acme-1-v1')::uuid, md5('ws-acme')::uuid, md5('kbtpl-acme-1')::uuid, 1, 'create', NULL, 'Release note', 'What changed and who it affects.',
     E'## What changed\n\nOne sentence.\n', md5('agent-acme-admin')::uuid, NULL, NULL, '2026-11-20 14:00:00+00'),
    (md5('kbtpl-acme-1-v2')::uuid, md5('ws-acme')::uuid, md5('kbtpl-acme-1')::uuid, 2, 'edit', NULL, 'Release note', 'What changed and who it affects.',
     E'## What changed\n\nTwo sentences.\n', md5('agent-acme-ben')::uuid, md5('token-acme-kb')::uuid, 'Knowledge base writer', '2026-11-20 14:10:00+00'),
    (md5('kbtpl-acme-1-v3')::uuid, md5('ws-acme')::uuid, md5('kbtpl-acme-1')::uuid, 3, 'restore', 1, 'Release note', 'What changed and who it affects.',
     E'## What changed\n\nOne sentence.\n', md5('agent-acme-ben')::uuid, NULL, NULL, '2026-11-20 14:20:00+00');
UPDATE kb_collections SET default_template_id = md5('kbtpl-acme-1')::uuid WHERE id = md5('kbc-acme-1')::uuid;

UPDATE kb_templates SET deleted_at = '2026-11-20 14:30:00+00', deleted_by_agent_id = md5('agent-beta-admin')::uuid,
        updated_at = '2026-11-20 14:30:00+00', updated_by_agent_id = md5('agent-beta-admin')::uuid
    WHERE workspace_id = md5('ws-beta')::uuid AND name = 'FAQ';
INSERT INTO kb_template_versions (id, workspace_id, template_id, version, kind, name, description, body, changed_by_agent_id, created_at)
    SELECT md5('kbtpl-beta-faq-v2')::uuid, workspace_id, id, 2, 'delete', name, description, body, md5('agent-beta-admin')::uuid, '2026-11-20 14:30:00+00'
    FROM kb_templates WHERE workspace_id = md5('ws-beta')::uuid AND name = 'FAQ';
