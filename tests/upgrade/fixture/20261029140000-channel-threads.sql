-- beta's one-to-one chat with a customer: the customer's message (beta's processed form event stands in
-- for a chat event), a reply from Bea, and a link Bea took back. Values are fixture data, not customers'.
INSERT INTO channel_threads (id, workspace_id, channel_id, kind, external_id, name, contact_id, opens_conversations, last_item_at, created_at, updated_at)
    SELECT md5('thread-beta-1')::uuid, e.workspace_id, e.channel_id, 'direct', 'user:4410233', 'Fixture chat', c.contact_id, true,
        '2026-10-25 12:05:00+00', '2026-10-25 12:00:00+00', '2026-10-25 12:05:00+00'
    FROM channel_inbound_events e JOIN conversations c ON c.workspace_id = e.workspace_id AND c.id = e.conversation_id
    WHERE e.id = md5('chin-beta-1')::uuid;
INSERT INTO channel_thread_items (id, workspace_id, channel_id, thread_id, direction, role, sender_kind, sender_value, sender_name, contact_id, agent_id,
        external_id, quote_external_id, body, inbound_event_id, message_id, gathering_id, disposition, disposition_reason, occurred_at, created_at, alerted_at)
    SELECT md5('item-beta-1')::uuid, e.workspace_id, e.channel_id, md5('thread-beta-1')::uuid, 'in', 'customer', 'chat_user', '4410233', 'Minh',
        c.contact_id, md5('agent-beta-admin')::uuid, 'chat-1', 'chat-0', 'Pricing?', e.id, e.message_id, NULL,
        'conversation', 'gathered', '2026-10-25 12:00:00+00', '2026-10-25 12:00:00+00', '2026-10-25 12:15:00+00'
    FROM channel_inbound_events e JOIN conversations c ON c.workspace_id = e.workspace_id AND c.id = e.conversation_id
    WHERE e.id = md5('chin-beta-1')::uuid;
INSERT INTO conversation_thread_items (id, workspace_id, conversation_id, item_id, reason, decision_id, linked_by_agent_id, linked_at, unlinked_at, unlinked_by_agent_id)
    SELECT md5('cti-beta-1')::uuid, e.workspace_id, e.conversation_id, md5('item-beta-1')::uuid, 'agent', md5('decision-beta-1')::uuid,
        md5('agent-beta-admin')::uuid, '2026-10-25 12:01:00+00', '2026-10-25 12:02:00+00', md5('agent-beta-admin')::uuid
    FROM channel_inbound_events e WHERE e.id = md5('chin-beta-1')::uuid;
INSERT INTO conversation_thread_items (id, workspace_id, conversation_id, item_id, reason, linked_at)
    SELECT md5('cti-beta-2')::uuid, e.workspace_id, e.conversation_id, md5('item-beta-1')::uuid, 'opened', '2026-10-25 12:03:00+00'
    FROM channel_inbound_events e WHERE e.id = md5('chin-beta-1')::uuid;
