-- Back to one help center per workspace: the primary stays with its domain, languages and
-- analytics; every other help center, with its own domain, languages, analytics and placements,
-- is removed.
DROP TRIGGER kb_collections_placed_on_primary ON kb_collections;
DROP FUNCTION kb_collection_placed_on_primary();
DROP TABLE kb_collection_help_centers;

DELETE FROM help_centers WHERE NOT primary_help_center;

DROP TRIGGER help_center_analytics_on_primary ON help_center_analytics;
ALTER TABLE help_center_analytics DROP CONSTRAINT help_center_analytics_help_center_id_fkey,
    DROP CONSTRAINT help_center_analytics_pkey,
    ADD PRIMARY KEY (workspace_id),
    DROP COLUMN help_center_id;

DROP TRIGGER help_center_languages_on_primary ON help_center_languages;
ALTER TABLE help_center_languages DROP CONSTRAINT help_center_languages_help_center_id_fkey,
    DROP CONSTRAINT help_center_languages_pkey,
    ADD PRIMARY KEY (workspace_id, locale),
    DROP COLUMN help_center_id;

DROP TRIGGER help_center_domains_on_primary ON help_center_domains;
ALTER TABLE help_center_domains DROP CONSTRAINT help_center_domains_help_center_id_fkey,
    DROP CONSTRAINT help_center_domains_pkey,
    ADD PRIMARY KEY (workspace_id),
    DROP COLUMN help_center_id;

DROP FUNCTION help_center_row_on_primary();
DROP FUNCTION ensure_primary_help_center(uuid);
DROP FUNCTION primary_help_center_id(uuid);

DROP INDEX help_centers_handle;
DROP INDEX help_centers_primary;
ALTER TABLE help_centers DROP CONSTRAINT help_centers_workspace_id_id_key,
    DROP CONSTRAINT help_centers_pkey,
    ADD PRIMARY KEY (workspace_id),
    DROP COLUMN deleted_at,
    DROP COLUMN primary_help_center,
    DROP COLUMN handle,
    DROP COLUMN id;
