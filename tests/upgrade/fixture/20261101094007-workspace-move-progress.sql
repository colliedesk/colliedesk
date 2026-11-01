-- A confirmed preview and opting out of a custom domain belong only to this workspace.
UPDATE workspace_onboarding SET move_steps='{"preview":"done","custom_domain":"skipped"}'::jsonb
WHERE workspace_id=md5('ws-acme')::uuid;
