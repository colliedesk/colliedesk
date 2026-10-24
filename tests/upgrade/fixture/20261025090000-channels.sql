-- The migration made beta's email and web form and cobalt's email into channels. acme removed the
-- email channel it had connected and set up a web form it then turned off, which held a message that
-- arrived while off. beta's channels received, collected, dropped, failed and discarded events, one of
-- each, and sealed a secret. cobalt knows one customer only by a chat id. The sealed secret is fixture
-- bytes, not a secret that opens.
INSERT INTO channels (id, workspace_id, type, singleton, name, enabled, settings, external_account, created_by_agent_id,
        created_at, updated_at, disabled_at, disabled_by_agent_id, deleted_at, deleted_by_agent_id) VALUES
    (md5('channel-acme-1')::uuid, md5('ws-acme')::uuid, 'email', true, 'Old support mailbox', false, '{}', 'support@acme.example',
     md5('agent-acme-admin')::uuid, '2026-10-20 09:00:00+00', '2026-10-24 09:00:00+00',
     '2026-10-24 09:00:00+00', md5('agent-acme-admin')::uuid, '2026-10-24 09:00:00+00', md5('agent-acme-admin')::uuid),
    (md5('channel-acme-2')::uuid, md5('ws-acme')::uuid, 'web_form', true, 'Website form', false,
     '{"join_policy": "never", "default_tags": ["web-form"]}', NULL,
     md5('agent-acme-admin')::uuid, '2026-10-24 10:00:00+00', '2026-10-24 11:00:00+00',
     '2026-10-24 11:00:00+00', md5('agent-acme-alex')::uuid, NULL, NULL);

INSERT INTO web_form_channels (workspace_id, enabled, allowed_origins, created_at, updated_at, channel_id) VALUES
    (md5('ws-acme')::uuid, false, ARRAY['https://acme.example'], '2026-10-24 10:00:00+00', '2026-10-24 11:00:00+00', md5('channel-acme-2')::uuid);

INSERT INTO channel_events (id, workspace_id, channel_id, channel_type, channel_name, kind, actor_agent_id, actor_token_id, detail, created_at) VALUES
    (md5('chev-acme-1')::uuid, md5('ws-acme')::uuid, md5('channel-acme-1')::uuid, 'email', 'Old support mailbox', 'added',
     md5('agent-acme-admin')::uuid, NULL, '{}', '2026-10-20 09:00:00+00'),
    (md5('chev-acme-2')::uuid, md5('ws-acme')::uuid, md5('channel-acme-1')::uuid, 'email', 'Old support mailbox', 'removed',
     md5('agent-acme-admin')::uuid, md5('session-acme-admin')::uuid, '{}', '2026-10-24 09:00:00+00'),
    (md5('chev-acme-3')::uuid, md5('ws-acme')::uuid, md5('channel-acme-2')::uuid, 'web_form', 'Website form', 'disabled',
     md5('agent-acme-alex')::uuid, NULL, '{}', '2026-10-24 11:00:00+00');

INSERT INTO channel_credentials (workspace_id, channel_id, name, key_id, sealed, hint, expires_at, version, updated_by_agent_id, updated_at)
    SELECT workspace_id, id, 'access_token', 'k1', '\x16171819'::bytea, 'Qm9z', '2026-11-24 10:00:00+00', 2,
        md5('agent-beta-admin')::uuid, '2026-10-24 10:00:00+00'
    FROM channels WHERE workspace_id = md5('ws-beta')::uuid AND type = 'email';

INSERT INTO contacts (id, workspace_id, name, email, created_at, updated_at) VALUES
    (md5('contact-cobalt-chat-1')::uuid, md5('ws-cobalt')::uuid, 'Minh', NULL, '2026-10-24 12:00:00+00', '2026-10-24 12:00:00+00');
INSERT INTO contact_identities (id, workspace_id, contact_id, channel_id, kind, value, display_name, created_at, updated_at)
    SELECT md5('cid-cobalt-1')::uuid, workspace_id, md5('contact-cobalt-chat-1')::uuid, id, 'chat_user', '4410233', 'Minh',
        '2026-10-24 12:00:00+00', '2026-10-24 12:00:00+00'
    FROM channels WHERE workspace_id = md5('ws-cobalt')::uuid AND type = 'email';

-- beta's newest web form message as the channel now writes it.
UPDATE messages m SET channel_id = c.id, channel_meta = '{"source": "pricing"}'
    FROM channels c
    WHERE c.workspace_id = m.workspace_id AND c.type = 'web_form'
        AND m.workspace_id = md5('ws-beta')::uuid AND m.id = (
            SELECT m2.id FROM messages m2 JOIN conversations v ON v.workspace_id = m2.workspace_id AND v.id = m2.conversation_id
            WHERE m2.workspace_id = md5('ws-beta')::uuid AND v.channel = 'web_form' AND m2.kind = 'incoming'
            ORDER BY m2.created_at DESC, m2.id DESC LIMIT 1);

INSERT INTO conversation_thread_keys (workspace_id, conversation_id, channel_type, key, created_at)
    SELECT workspace_id, conversation_id, 'web_form', 'form:pricing', '2026-10-24 12:00:00+00'
    FROM messages WHERE workspace_id = md5('ws-beta')::uuid AND channel_meta IS NOT NULL;

UPDATE channel_outbox SET external_id = 'email:' || (SELECT email_message_id FROM messages m
        WHERE m.workspace_id = channel_outbox.workspace_id AND m.id = channel_outbox.message_id),
    meta = '{"transport": "smtp"}'
    WHERE workspace_id = md5('ws-beta')::uuid AND status = 'sent';
UPDATE channel_outbox SET last_error_code = 'send_failed' WHERE workspace_id = md5('ws-beta')::uuid AND status = 'failed';

-- One event in each state that keeps one, on beta's channels and acme's form.
INSERT INTO channel_inbound_events (id, workspace_id, channel_id, external_event_id, state, raw, raw_object_key, sender_key, hold_reason,
        aggregate_key, flush_at, drop_reason, error, attempts, conversation_id, message_id, decided_by_agent_id, received_at, processed_at)
    SELECT md5('chin-beta-1')::uuid, m.workspace_id, m.channel_id, 'form:' || m.id, 'processed',
        '{"email": "buyer@example.com", "message": "Pricing?", "source": "pricing"}', NULL, 'buyer@example.com', NULL,
        NULL, NULL, NULL, NULL, 1, m.conversation_id, m.id, NULL, m.created_at, m.created_at
    FROM messages m WHERE m.workspace_id = md5('ws-beta')::uuid AND m.channel_meta IS NOT NULL;
INSERT INTO channel_inbound_events (id, workspace_id, channel_id, external_event_id, state, raw, raw_object_key, raw_bytes, hold_reason,
        aggregate_key, flush_at, drop_reason, error, attempts, conversation_id, message_id, decided_by_agent_id, received_at, processed_at)
    SELECT md5('chin-beta-' || n)::uuid, c.workspace_id, c.id, 'mail:' || n, state, raw::jsonb, object_key,
        CASE WHEN object_key IS NOT NULL THEN 812 END, NULL,
        aggregate_key, flush_at::timestamptz, drop_reason, error, attempts, NULL, NULL, decided_by::uuid, '2026-10-24 13:00:00+00', processed_at::timestamptz
    FROM channels c, (VALUES
        ('2', 'dropped', NULL, (SELECT storage_key FROM attachments WHERE workspace_id = md5('ws-beta')::uuid ORDER BY storage_key LIMIT 1),
         NULL, NULL, 'own_address', NULL, 1, NULL, '2026-10-24 13:00:01+00'),
        ('3', 'pending', '{"text": "and one more thing"}', NULL, 'user:4410233', '2026-10-24 13:00:10+00', NULL, NULL, 0, NULL, NULL),
        ('4', 'failed', '{"text": "broken"}', NULL, NULL, NULL, NULL, 'store inbound message: timeout', 3, NULL, NULL),
        ('5', 'discarded', '{"text": "spam"}', NULL, NULL, NULL, NULL, NULL, 1, md5('agent-beta-admin')::text, '2026-10-24 13:05:00+00')
    ) AS e(n, state, raw, object_key, aggregate_key, flush_at, drop_reason, error, attempts, decided_by, processed_at)
    WHERE c.workspace_id = md5('ws-beta')::uuid AND c.type = 'email';
INSERT INTO channel_inbound_events (id, workspace_id, channel_id, external_event_id, state, raw, hold_reason, received_at) VALUES
    (md5('chin-acme-1')::uuid, md5('ws-acme')::uuid, md5('channel-acme-2')::uuid, NULL, 'held',
     '{"email": "visitor@example.com", "message": "Are you open on Sunday?", "source": "contact"}', 'channel_off', '2026-10-24 12:30:00+00');

-- The collected beta event had two files: one fetched into storage, one whose link was dead.
INSERT INTO channel_inbound_files (workspace_id, event_id, idx, name, content_type, status, storage_key, sha256, size, error, attempts, attached,
        created_at, updated_at)
    SELECT md5('ws-beta')::uuid, md5('chin-beta-3')::uuid, 0, 'screenshot.png', 'image/png', 'stored', a.storage_key, a.sha256, a.size, NULL, 1, false,
        '2026-10-24 13:00:02+00', '2026-10-24 13:00:02+00'
    FROM attachments a WHERE a.workspace_id = md5('ws-beta')::uuid ORDER BY a.storage_key LIMIT 1;
INSERT INTO channel_inbound_files (workspace_id, event_id, idx, name, content_type, status, storage_key, sha256, size, error, attempts, attached,
        created_at, updated_at) VALUES
    (md5('ws-beta')::uuid, md5('chin-beta-3')::uuid, 1, 'voice.amr', 'audio/amr', 'failed', NULL, NULL, NULL, 'the link answered 404', 3, false,
     '2026-10-24 13:00:03+00', '2026-10-24 13:00:09+00');

-- A sent reply that the channel delivered as two messages.
INSERT INTO channel_sent_ids (workspace_id, channel_id, external_id, message_id, created_at)
    SELECT o.workspace_id, o.channel_id, 'part-' || n || '-' || o.message_id, o.message_id, o.sent_at
    FROM channel_outbox o, generate_series(1, 2) n
    WHERE o.workspace_id = md5('ws-beta')::uuid AND o.status = 'sent' AND o.id = (
        SELECT id FROM channel_outbox WHERE workspace_id = md5('ws-beta')::uuid AND status = 'sent' ORDER BY id LIMIT 1);
