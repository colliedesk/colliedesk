-- acme's provider routes by data policy, its intake falls back on a second model and runs 120
-- decisions a day; one of acme's calls was reported at a cost in the provider's credits.
UPDATE ai_providers SET routing = '{"data_collection": "deny", "zdr": true}' WHERE id = md5('aip-acme-1')::uuid;
INSERT INTO ai_slots (workspace_id, slot, uses_server, provider_id, model, fallback_model, effort, enabled, default_level, auto_parts, consented_by, consented_at, updated_by, updated_at)
    SELECT workspace_id, 'intake', false, id, models[1], models[1], 'low', true, 'suggest', '{}', md5('agent-acme-admin')::uuid,
        '2026-10-25 13:00:00+00', md5('agent-acme-admin')::uuid, '2026-10-25 13:00:00+00'
    FROM ai_providers WHERE id = md5('aip-acme-1')::uuid;
UPDATE ai_automatic_limits SET intake_per_day = 120 WHERE workspace_id = md5('ws-acme')::uuid;
UPDATE ai_usage SET provider_cost = 0.001234 WHERE id = md5('aiu-acme-1')::uuid;
