-- Going back means the previous release, which reads its settings from the environment again. Export
-- what was set here first (`collie config export --format env --include-secrets`, docs/self-host/upgrade.md).
DROP TABLE IF EXISTS operator_audit;
DROP FUNCTION IF EXISTS operator_audit_append_only();
DROP TABLE IF EXISTS polar_products;
DROP FUNCTION IF EXISTS polar_products_add_only();
DROP TABLE IF EXISTS server_processes;
DROP TABLE IF EXISTS server_config_state;
DROP TABLE IF EXISTS server_config_values;
DROP TABLE IF EXISTS server_config_versions;
