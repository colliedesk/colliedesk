-- Replies copied to other addresses.
UPDATE messages SET cc = ARRAY['boss@beta-users.example']
WHERE workspace_id = md5('ws-beta')::uuid AND id IN (md5('msg-beta-2-out')::uuid, md5('msg-beta-4-out')::uuid);
UPDATE messages SET cc = ARRAY['a@beta-users.example', 'b@beta-users.example']
WHERE workspace_id = md5('ws-beta')::uuid AND id = md5('msg-beta-6-out')::uuid;
