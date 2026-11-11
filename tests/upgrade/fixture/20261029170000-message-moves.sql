-- beta: Bea moved the customer message of Question 11 into Question 12 and closed 11 as no new request,
-- undid an earlier move through a token, and took a chat message back to the intake queue. Values are
-- fixture data, not customers'.
UPDATE messages SET conversation_id = md5('conv-beta-12')::uuid, other_contact_id = md5('contact-beta-12')::uuid
    WHERE id = md5('msg-beta-11-in')::uuid;
UPDATE messages SET detached_at = '2026-10-26 09:10:00+00', detached_by_agent_id = md5('agent-beta-admin')::uuid
    WHERE id = md5('msg-beta-13-in')::uuid;
UPDATE conversations SET status = 'closed', merged_into_id = md5('conv-beta-12')::uuid WHERE id = md5('conv-beta-11')::uuid;
INSERT INTO conversation_thread_keys (workspace_id, conversation_id, channel_type, key, message_id, created_at)
    VALUES (md5('ws-beta')::uuid, md5('conv-beta-12')::uuid, 'web_form', 'form:moved', md5('msg-beta-11-in')::uuid, '2026-10-26 09:00:00+00');
INSERT INTO message_moves (id, workspace_id, message_ids, from_conversation_id, to_conversation_id, created_conversation, closed_source,
        source_status_before, target_status_before, thread_keys_moved, gathering_id, decision_id, moved_by_agent_id, moved_by_token_id, moved_at,
        undone_at, undone_by_agent_id, undone_by_token_id)
    VALUES
    (md5('move-beta-1')::uuid, md5('ws-beta')::uuid, ARRAY[md5('msg-beta-11-in')::uuid], md5('conv-beta-11')::uuid, md5('conv-beta-14')::uuid, true, false,
        NULL, NULL, '[]', NULL, md5('decision-beta-2')::uuid, md5('agent-beta-admin')::uuid, md5('token-beta-1')::uuid, '2026-10-26 08:50:00+00',
        '2026-10-26 08:55:00+00', md5('agent-beta-admin')::uuid, md5('token-beta-1')::uuid),
    (md5('move-beta-2')::uuid, md5('ws-beta')::uuid, ARRAY[md5('msg-beta-11-in')::uuid], md5('conv-beta-11')::uuid, md5('conv-beta-12')::uuid, false, true,
        'pending', 'resolved', '[{"channel_type": "web_form", "key": "form:moved"}]', NULL, NULL, md5('agent-beta-admin')::uuid, NULL,
        '2026-10-26 09:00:00+00', NULL, NULL, NULL),
    (md5('move-beta-3')::uuid, md5('ws-beta')::uuid, ARRAY[md5('msg-beta-13-in')::uuid], md5('conv-beta-13')::uuid, NULL, false, false,
        NULL, NULL, '[]', md5('gathering-beta-2')::uuid, NULL, md5('agent-beta-admin')::uuid, NULL, '2026-10-26 09:10:00+00', NULL, NULL, NULL);
