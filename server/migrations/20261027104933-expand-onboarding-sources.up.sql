-- Keep acquisition self-reported and single-select, with one key per AI assistant.
ALTER TABLE workspace_onboarding DROP CONSTRAINT workspace_onboarding_source_check;
ALTER TABLE workspace_onboarding ADD CONSTRAINT workspace_onboarding_source_check
    CHECK (source IN ('x', 'email', 'search', 'ai_chatgpt', 'ai_claude', 'ai_gemini',
        'ai_perplexity', 'ai_other', 'tiktok', 'youtube', 'reddit', 'github', 'referral', 'other'));
