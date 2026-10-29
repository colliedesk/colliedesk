-- Results: the first workspace's baseline import was unverified only because its Freshdesk plan refuses the archive API.
UPDATE results_state SET baseline_import_status = 'UNVERIFIED', baseline_import_cause = 'archive_refused' WHERE workspace_id = md5('ws-acme')::uuid;
