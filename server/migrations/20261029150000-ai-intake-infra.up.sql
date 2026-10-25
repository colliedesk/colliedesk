-- What intake needs of the AI layer (docs/internals/ai.md): the cost a provider reports itself, a
-- model to fall back on, OpenRouter's routing by data policy, and a daily limit of its own.

-- What the provider says a call cost, in its own unit (OpenRouter: credits), never converted: it is
-- not the server's price (cost_micro_usd), which only the server's provider has.
ALTER TABLE ai_usage ADD COLUMN provider_cost numeric(20, 9) CHECK (provider_cost >= 0);

-- The model intake tries when its model fails or answers what cannot be read; one of the same
-- provider's models.
ALTER TABLE ai_slots ADD COLUMN fallback_model text CHECK (char_length(fallback_model) BETWEEN 1 AND 200),
    ADD CHECK (fallback_model IS NULL OR slot = 'intake');

-- OpenRouter's provider routing by data policy: {"data_collection": "deny", "zdr": true}. Other
-- servers ignore it.
ALTER TABLE ai_providers ADD COLUMN routing jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(routing) = 'object' AND octet_length(routing::text) <= 1024);

-- Intake decisions a UTC day, counted inside per_day, so a busy group cannot spend the triage and
-- drafts of the day.
ALTER TABLE ai_automatic_limits ADD COLUMN intake_per_day integer NOT NULL DEFAULT 300 CHECK (intake_per_day BETWEEN 0 AND 10000);
