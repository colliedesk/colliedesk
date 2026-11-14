-- acme's admin first opened the sign-in page from a link on claude.ai; beta came from no outside site.
UPDATE workspace_onboarding SET referrer_host = 'claude.ai' WHERE workspace_id = md5('ws-acme')::uuid;
