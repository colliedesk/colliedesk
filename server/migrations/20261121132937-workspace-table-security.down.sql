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

DROP FUNCTION limit_app_role(regclass, text[]);
DROP FUNCTION secure_server_table(regclass);
DROP FUNCTION secure_workspace_table(regclass);
DROP FUNCTION owned_sequences(regclass);
DROP FUNCTION app_role();
