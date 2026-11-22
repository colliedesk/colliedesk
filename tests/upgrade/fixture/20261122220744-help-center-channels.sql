-- acme's website form now serves its hardware help center, and the request it opened is marked as
-- coming from there.
UPDATE channels SET help_center_id = md5('hc-acme-2')::uuid
WHERE workspace_id = md5('ws-acme')::uuid AND id = md5('channel-acme-2')::uuid;
UPDATE conversations SET help_center_id = md5('hc-acme-2')::uuid
WHERE workspace_id = md5('ws-acme')::uuid AND id = md5('conv-acme-1')::uuid;
