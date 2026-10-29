-- AI chat: the first workspace turned the chat slot on with a monthly limit and has a help chat
-- channel embedded on one site. One chat answered twice from articles (one answer rated not helpful,
-- one article since deleted), was handed to the team as conversation 1 and moved once from the
-- widget; a second chat ended after a failed turn. The articles' size is known for one model.
INSERT INTO channels (id, workspace_id, type, name, enabled, settings, created_by_agent_id, created_at, updated_at, is_default) VALUES
    (md5('channel-acme-chat')::uuid, md5('ws-acme')::uuid, 'help_chat', 'Help chat', true, '{}', md5('agent-acme-admin')::uuid,
     '2026-10-28 09:00:00+00', '2026-10-28 09:00:00+00', true);

INSERT INTO help_chat_channels (workspace_id, channel_id, greeting, suggestions, instructions, widget_origins, created_at, updated_at) VALUES
    (md5('ws-acme')::uuid, md5('channel-acme-chat')::uuid, 'Hi! Ask about orders and delivery.', ARRAY['Where is my order?', 'How do I reset my password?'],
     'Answer in a friendly tone.', ARRAY['https://shop.acme.example'], '2026-10-28 09:00:00+00', '2026-10-28 09:05:00+00');

INSERT INTO ai_slots (workspace_id, slot, uses_server, provider_id, model, effort, enabled, consented_by, consented_at, updated_by, updated_at, monthly_cap_micro_usd) VALUES
    (md5('ws-acme')::uuid, 'chat', false, md5('aip-acme-1')::uuid, 'claude-haiku-5-5', 'low', true,
     md5('agent-acme-admin')::uuid, '2026-10-28 09:01:00+00', md5('agent-acme-admin')::uuid, '2026-10-28 09:01:00+00', 10000000);

INSERT INTO ai_chat_sessions (id, workspace_id, channel_id, token_hash, surface, status, conversation_id, history_cut_at, created_at, last_turn_at, ended_at) VALUES
    (md5('chat-acme-1')::uuid, md5('ws-acme')::uuid, md5('channel-acme-chat')::uuid, sha256('token-acme-1'), 'widget', 'handed_off', md5('conv-acme-1')::uuid,
     '2026-10-28 10:00:00+00', '2026-10-28 09:30:00+00', '2026-10-28 10:05:00+00', NULL),
    (md5('chat-acme-2')::uuid, md5('ws-acme')::uuid, md5('channel-acme-chat')::uuid, sha256('token-acme-2'), 'article', 'ended', NULL,
     NULL, '2026-10-28 11:00:00+00', '2026-10-28 11:01:00+00', '2026-10-28 11:02:00+00');

INSERT INTO ai_chat_turns (id, workspace_id, session_id, seq, question, answer, viewing_kb_article_id, status, error_code, model, ttft_ms,
        input_tokens, cache_read_tokens, cache_creation_tokens, output_tokens, created_at, finished_at) VALUES
    (md5('turn-acme-1')::uuid, md5('ws-acme')::uuid, md5('chat-acme-1')::uuid, 1, 'How do I reset my password?', 'Use the reset link [#1].', md5('kba-acme-1')::uuid,
     'answered', NULL, 'claude-haiku-5-5', 1300, 50, 39000, 0, 40, '2026-10-28 09:30:00+00', '2026-10-28 09:30:03+00'),
    (md5('turn-acme-2')::uuid, md5('ws-acme')::uuid, md5('chat-acme-1')::uuid, 2, 'And on the app?', 'The articles do not say.', NULL,
     'no_source', NULL, 'claude-haiku-5-5', 1200, 60, 39050, 0, 20, '2026-10-28 10:04:00+00', '2026-10-28 10:04:02+00'),
    (md5('turn-acme-3')::uuid, md5('ws-acme')::uuid, md5('chat-acme-2')::uuid, 1, 'Refund?', NULL, NULL,
     'failed', 'timeout', 'claude-haiku-5-5', NULL, NULL, NULL, NULL, NULL, '2026-10-28 11:00:00+00', '2026-10-28 11:01:00+00');

INSERT INTO ai_chat_sources (workspace_id, turn_id, ordinal, kb_article_id, article_number, article_version, title, folder) VALUES
    (md5('ws-acme')::uuid, md5('turn-acme-1')::uuid, 1, md5('kba-acme-1')::uuid, 1, 3, 'Reset your password', 'Account'),
    (md5('ws-acme')::uuid, md5('turn-acme-1')::uuid, 2, NULL, 7, 1, 'An article since deleted', NULL);

INSERT INTO ai_chat_feedback (workspace_id, turn_id, rating, created_at, updated_at) VALUES
    (md5('ws-acme')::uuid, md5('turn-acme-2')::uuid, 'not_helpful', '2026-10-28 10:04:30+00', '2026-10-28 10:04:40+00');

INSERT INTO ai_chat_kb_snapshots (workspace_id, fingerprint, model, kb_tokens, system_tokens, whole, last_cache_at, created_at) VALUES
    (md5('ws-acme')::uuid, sha256('kb-acme-set-1'), 'claude-haiku-5-5', 39279, 900, true, '2026-10-28 10:04:00+00', '2026-10-28 09:29:00+00');

INSERT INTO ai_chat_transfers (workspace_id, session_id, code_hash, expires_at, used_at) VALUES
    (md5('ws-acme')::uuid, md5('chat-acme-1')::uuid, sha256('code-acme-1'), '2026-10-28 09:42:00+00', '2026-10-28 09:40:30+00');

INSERT INTO ai_settings_events (workspace_id, kind, actor_agent_id, provider_id, provider_label, slot, detail, created_at) VALUES
    (md5('ws-acme')::uuid, 'slot_changed', md5('agent-acme-admin')::uuid, md5('aip-acme-1')::uuid, 'Team key', 'chat', '{"enabled": true}', '2026-10-28 09:01:00+00');
