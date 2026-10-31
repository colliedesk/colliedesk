-- beta's send token resolved quietly a conversation, which closed by itself on acme's web form after the form
-- began closing resolved conversations.
INSERT INTO conversation_status_changes (workspace_id, id, conversation_id, from_status, to_status, changed_at, actor_agent_id, source, cause, actor_token_id, quiet) VALUES
    (md5('ws-beta')::uuid, md5('status-change-beta-2')::uuid, md5('conv-beta-1')::uuid, 'closed', 'resolved', now() - interval '12 hours', md5('agent-beta-admin')::uuid, 'colliedesk', 'token', md5('token-beta-send')::uuid, true),
    (md5('ws-beta')::uuid, md5('status-change-beta-3')::uuid, md5('conv-beta-1')::uuid, 'resolved', 'closed', now() - interval '1 hour', NULL, 'colliedesk', 'auto_close', NULL, false);
UPDATE channels SET auto_close_since = '2026-10-31 17:00:00+00', settings = settings || '{"auto_close_hours": 48}' WHERE id = md5('channel-acme-2')::uuid;
-- acme's bulk resolve has a notice still to tell.
UPDATE conversations SET status_notice_due = status WHERE id = md5('conv-acme-1')::uuid;
