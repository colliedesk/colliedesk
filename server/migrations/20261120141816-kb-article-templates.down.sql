-- Every template and its history goes; articles made from a template keep their text.
DROP FUNCTION kb_seed_starter_templates(uuid);
ALTER TABLE workspaces DROP COLUMN kb_starter_templates_at;
ALTER TABLE kb_collections DROP COLUMN default_template_id;
DROP TABLE kb_template_versions;
DROP TABLE kb_templates;
