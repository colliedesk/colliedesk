-- The schema before has no room for Autofix. Refuse to roll back while a workspace holds a fix attempt
-- or a GitHub connection, naming the workspaces, rather than drop what was sent and where it went (the
-- way back for a server is the backup). Settings alone (connectors, repositories never used) go.
DO $$
DECLARE
    holders text;
BEGIN
    SELECT string_agg(w.slug, ', ' ORDER BY w.slug) INTO holders FROM workspaces w
        WHERE EXISTS (SELECT 1 FROM fix_attempts f WHERE f.workspace_id = w.id)
            OR EXISTS (SELECT 1 FROM autofix_github_installations i WHERE i.workspace_id = w.id);
    IF holders IS NOT NULL THEN
        RAISE EXCEPTION 'Autofix holds fix attempts or a GitHub connection in workspaces %; roll back from a backup instead', holders;
    END IF;
END
$$;

DROP TABLE autofix_webhook_deliveries;
DROP TABLE autofix_oauth_states;
DROP TABLE fix_attempt_events;
DROP FUNCTION fix_attempt_events_append_only();
DROP TABLE fix_attempts;
DROP TABLE autofix_connectors;
DROP TABLE autofix_repos;
DROP TABLE autofix_github_installations;
