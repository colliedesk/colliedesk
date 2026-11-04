-- acme's sign-up started on the Zendesk comparison page; beta came from no link and keeps it empty.
UPDATE workspace_onboarding SET landing_path = '/vs/zendesk' WHERE workspace_id = md5('ws-acme')::uuid;
