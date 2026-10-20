-- Two contacts of one person merged (their conversations moved), and a merge that was undone.
UPDATE conversations SET contact_id = md5('contact-acme-2')::uuid
WHERE workspace_id = md5('ws-acme')::uuid AND contact_id = md5('contact-acme-3')::uuid;
UPDATE contacts SET merged_into_id = md5('contact-acme-2')::uuid WHERE workspace_id = md5('ws-acme')::uuid AND id = md5('contact-acme-3')::uuid;
INSERT INTO contact_merges (id, workspace_id, contact_id, merged_contact_id, conversation_ids, merged_by_agent_id, token_id, created_at, undone_at, undone_by_agent_id) VALUES
    (md5('merge-acme-1')::uuid, md5('ws-acme')::uuid, md5('contact-acme-2')::uuid, md5('contact-acme-3')::uuid,
     ARRAY[md5('conv-acme-2')::uuid, md5('conv-acme-32')::uuid], md5('agent-acme-admin')::uuid, NULL, '2026-10-19 10:00:00+00', NULL, NULL),
    (md5('merge-beta-undone')::uuid, md5('ws-beta')::uuid, md5('contact-beta-4')::uuid, md5('contact-beta-5')::uuid,
     '{}', md5('agent-beta-cy')::uuid, md5('token-beta-draft')::uuid, '2026-10-19 10:05:00+00', '2026-10-19 10:10:00+00', md5('agent-beta-admin')::uuid);
