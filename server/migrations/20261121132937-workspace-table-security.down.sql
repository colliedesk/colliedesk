DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['saved_reply_versions', 'roles', 'agent_requests', 'response_targets',
        'contact_merges', 'conversation_number_series', 'conversation_number_formats'] LOOP
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO %I', t, app_role());
    END LOOP;
END
$$;
