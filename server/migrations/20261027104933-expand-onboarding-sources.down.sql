-- Refuse rollback while new sources are in use: mapping or deleting them would lose attribution.
-- Leave those answers intact and keep this migration applied until they can be preserved elsewhere.
-- Mark the transaction privileged so the refusal also sees rows protected by row-level security.
DO $$
DECLARE
    in_use bigint;
BEGIN
    PERFORM set_config('app.privileged', 'on', true);
    SELECT count(*) INTO in_use FROM workspace_onboarding
        WHERE source IN ('ai_chatgpt', 'ai_claude', 'ai_gemini', 'ai_perplexity', 'ai_other', 'tiktok', 'youtube');
    IF in_use > 0 THEN
        RAISE EXCEPTION 'cannot roll back: % onboarding answer(s) use AI assistant, TikTok or YouTube sources; preserve attribution before rollback', in_use;
    END IF;
END
$$;
ALTER TABLE workspace_onboarding DROP CONSTRAINT workspace_onboarding_source_check;
ALTER TABLE workspace_onboarding ADD CONSTRAINT workspace_onboarding_source_check
    CHECK (source IN ('x', 'email', 'search', 'reddit', 'github', 'referral', 'other'));
