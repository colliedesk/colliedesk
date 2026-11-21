-- Removes the table security helpers. Run it only after the migrations that call them are
-- rolled back.
DROP FUNCTION limit_app_role(regclass, text[]);
DROP FUNCTION secure_server_table(regclass);
DROP FUNCTION secure_workspace_table(regclass);
DROP FUNCTION owned_sequences(regclass);
DROP FUNCTION app_role();
