-- acme added its own Anthropic key and an OpenAI-compatible endpoint that failed its last check; its
-- reply drafts run on its key and triage on the server's provider. beta added a key but left every
-- feature off. The sealed keys are fixture bytes, not keys that open.
INSERT INTO ai_providers (id, workspace_id, kind, label, base_url, auth_kind, sealed_key, sealed_key_id, key_hint, models,
        check_status, check_error_code, checked_at, created_by, created_at, updated_at) VALUES
    (md5('aip-acme-1')::uuid, md5('ws-acme')::uuid, 'anthropic', 'Anthropic — team key', NULL, 'api_key', '\x0a0b0c0d'::bytea, 'k1', '7Qx2',
     ARRAY['claude-sonnet-5-5', 'claude-haiku-5-5'], 'ok', NULL, '2026-10-22 11:00:00+00',
     md5('agent-acme-admin')::uuid, '2026-10-22 11:00:00+00', '2026-10-22 11:00:00+00'),
    (md5('aip-acme-2')::uuid, md5('ws-acme')::uuid, 'openai_compatible', 'OpenRouter', 'https://openrouter.ai/api/v1', 'api_key', '\x0e0f1011'::bytea, 'k1', 'a91f',
     ARRAY['openai/gpt-5'], 'failing', 'auth', '2026-10-22 11:30:00+00',
     md5('agent-acme-admin')::uuid, '2026-10-22 11:10:00+00', '2026-10-22 11:30:00+00'),
    (md5('aip-acme-3')::uuid, md5('ws-acme')::uuid, 'openai_compatible', 'Office Ollama', 'https://llm.acme.example/v1', 'none', NULL, NULL, NULL,
     ARRAY['llama-4-scout'], 'ok', NULL, '2026-10-22 11:20:00+00',
     md5('agent-acme-alex')::uuid, '2026-10-22 11:20:00+00', '2026-10-22 11:20:00+00'),
    (md5('aip-beta-1')::uuid, md5('ws-beta')::uuid, 'anthropic', 'Claude', NULL, 'api_key', '\x12131415'::bytea, 'k1', 'Zz01',
     ARRAY['claude-haiku-5-5'], 'ok', NULL, '2026-10-22 12:00:00+00',
     md5('agent-beta-admin')::uuid, '2026-10-22 12:00:00+00', '2026-10-22 12:00:00+00');

INSERT INTO ai_slots (workspace_id, slot, uses_server, provider_id, model, effort, enabled, consented_by, consented_at, updated_by, updated_at) VALUES
    (md5('ws-acme')::uuid, 'triage', true, NULL, 'claude-haiku-5-5', 'low', true,
     md5('agent-acme-admin')::uuid, '2026-10-22 11:05:00+00', md5('agent-acme-admin')::uuid, '2026-10-22 11:05:00+00'),
    (md5('ws-acme')::uuid, 'draft', false, md5('aip-acme-1')::uuid, 'claude-sonnet-5-5', 'medium', true,
     md5('agent-acme-admin')::uuid, '2026-10-22 11:06:00+00', md5('agent-acme-admin')::uuid, '2026-10-22 11:06:00+00'),
    (md5('ws-beta')::uuid, 'draft', false, md5('aip-beta-1')::uuid, 'claude-haiku-5-5', 'low', false,
     NULL, NULL, md5('agent-beta-admin')::uuid, '2026-10-22 12:01:00+00');

INSERT INTO ai_settings_events (id, workspace_id, kind, actor_agent_id, actor_token_id, provider_id, provider_label, slot, detail, created_at) VALUES
    (md5('aie-acme-1')::uuid, md5('ws-acme')::uuid, 'provider_added', md5('agent-acme-admin')::uuid, NULL, md5('aip-acme-1')::uuid, 'Anthropic — team key', NULL,
     '{"key_hint": "7Qx2"}', '2026-10-22 11:00:00+00'),
    (md5('aie-acme-2')::uuid, md5('ws-acme')::uuid, 'slot_changed', md5('agent-acme-admin')::uuid, md5('session-acme-admin')::uuid, md5('aip-acme-1')::uuid,
     'Anthropic — team key', 'draft', '{"from": {"enabled": false}, "to": {"enabled": true, "source": "provider", "model": "claude-sonnet-5-5"}}',
     '2026-10-22 11:06:00+00'),
    (md5('aie-beta-1')::uuid, md5('ws-beta')::uuid, 'provider_added', md5('agent-beta-admin')::uuid, NULL, md5('aip-beta-1')::uuid, 'Claude', NULL,
     '{"key_hint": "Zz01"}', '2026-10-22 12:00:00+00');

INSERT INTO ai_usage (id, workspace_id, feature, billed_to, provider_id, provider_kind, provider_label, model, status, error_code, stop_reason,
        served_model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, hops, provider_request_id, actor_agent_id, actor_token_id,
        started_at, finished_at, latency_ms) VALUES
    (md5('aiu-acme-1')::uuid, md5('ws-acme')::uuid, 'check', 'workspace_key', md5('aip-acme-1')::uuid, 'anthropic', 'Anthropic — team key',
     'claude-sonnet-5-5', 'ok', NULL, 'done', 'claude-sonnet-5-5', 40, 6, 0, 0,
     '[{"model": "claude-sonnet-5-5", "input": 40, "output": 6, "cache_read": 0, "cache_write": 0}]', 'req_fixture_1',
     md5('agent-acme-admin')::uuid, md5('session-acme-admin')::uuid, '2026-10-22 10:59:58+00', '2026-10-22 11:00:00+00', 1800),
    (md5('aiu-acme-2')::uuid, md5('ws-acme')::uuid, 'check', 'workspace_key', md5('aip-acme-2')::uuid, 'openai_compatible', 'OpenRouter',
     'openai/gpt-5', 'error', 'auth', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL,
     md5('agent-acme-admin')::uuid, NULL, '2026-10-22 11:29:59+00', '2026-10-22 11:30:00+00', 400),
    (md5('aiu-beta-1')::uuid, md5('ws-beta')::uuid, 'check', 'workspace_key', md5('aip-beta-1')::uuid, 'anthropic', 'Claude',
     'claude-haiku-5-5', 'ok', NULL, 'max_tokens', 'claude-haiku-5-5', 38, 256, 0, 0,
     '[{"model": "claude-haiku-5-5", "input": 38, "output": 256, "cache_read": 0, "cache_write": 0}]', 'req_fixture_2',
     md5('agent-beta-admin')::uuid, NULL, '2026-10-22 11:59:58+00', '2026-10-22 12:00:00+00', 2100);
