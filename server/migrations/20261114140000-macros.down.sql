SELECT set_config('app.privileged', 'on', true);
UPDATE roles SET permissions = (
    SELECT COALESCE(array_agg(permission ORDER BY ordinality), '{}')
    FROM unnest(permissions) WITH ORDINALITY AS item(permission, ordinality)
    WHERE permission NOT IN ('macros:read', 'macros:create', 'macros:update', 'macros:delete')
), updated_at = now()
WHERE system = 'agent' AND permissions && ARRAY['macros:read', 'macros:create', 'macros:update', 'macros:delete']::text[];

CREATE OR REPLACE FUNCTION colliedesk_agent_role_permissions() RETURNS text[] LANGUAGE sql IMMUTABLE AS $$
    SELECT ARRAY['conversations:read', 'conversations:update', 'messages:read', 'messages:note', 'messages:send',
        'ai_assist:read', 'ai_assist:create', 'contacts:read', 'contacts:create', 'contacts:update', 'tags:read',
        'tags:create', 'tags:update', 'tags:delete', 'saved_replies:read', 'saved_replies:create',
        'saved_replies:update', 'saved_replies:delete', 'saved_views:read', 'saved_views:create',
        'saved_views:update', 'saved_views:delete', 'kb_articles:read', 'kb_articles:create', 'kb_articles:update',
        'kb_articles:delete', 'kb_articles:publish', 'kb_media:read', 'kb_media:create', 'kb_media:update',
        'kb_media:delete', 'help_center:read', 'results:read', 'profile:update', 'team:read', 'channels:read',
        'ai_settings:read', 'autofix:read', 'autofix:update', 'autofix:send', 'autofix_settings:read',
        'team:create_tokens']::text[]
$$;

DROP TABLE macro_applications;
DROP TABLE macros;
