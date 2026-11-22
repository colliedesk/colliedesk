-- Removes the help center reader's policies, grants, views and the collection top column. The role
-- itself and its membership in the app role stay, like the app role in the workspace security
-- migration: other databases on the server may use the same name pattern, and a role is cheap.

DROP VIEW kb_help_center_public_translations;
DROP VIEW kb_help_center_public_articles;
DROP VIEW kb_help_center_collection_ranks;

DO $$
DECLARE
    t text;
    sequence regclass;
BEGIN
    FOREACH t IN ARRAY ARRAY['help_centers', 'help_center_languages', 'kb_collection_help_centers',
        'kb_collections', 'kb_collection_translations', 'kb_article_collections', 'kb_articles',
        'kb_article_translations', 'kb_article_media', 'kb_article_attachments', 'kb_article_tags',
        'kb_article_vote_resets', 'kb_article_votes', 'kb_article_vote_versions', 'kb_media',
        'import_items']
    LOOP
        EXECUTE format('DROP POLICY help_center_isolation ON %I', t);
        EXECUTE format('ALTER POLICY workspace_isolation ON %I TO %I', t, app_role());
        EXECUTE format('REVOKE ALL ON %I FROM %I', t, reader_role());
        FOR sequence IN SELECT owned_sequences(t::regclass) LOOP
            EXECUTE format('REVOKE ALL ON SEQUENCE %s FROM %I', sequence, reader_role());
        END LOOP;
    END LOOP;
    FOREACH t IN ARRAY ARRAY['kb_collection_visibility', 'kb_published_articles',
        'kb_published_translations']
    LOOP
        EXECUTE format('REVOKE ALL ON %I FROM %I', t, reader_role());
    END LOOP;
    EXECUTE format('REVOKE USAGE ON SCHEMA %I FROM %I', current_schema(), reader_role());
END
$$;

DROP TRIGGER kb_collections_children_top ON kb_collections;
DROP TRIGGER kb_collections_top ON kb_collections;
DROP FUNCTION kb_collection_children_top();
DROP FUNCTION kb_collection_top();
ALTER TABLE kb_collections DROP COLUMN top_collection_id;

DROP FUNCTION secure_help_center_table(regclass, text, text[]);
DROP FUNCTION reader_role();
