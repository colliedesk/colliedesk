-- The first colliedesk.com page of the visit a cloud sign-up came from (landing_path), kept with the post
-- or ad of the link so trials and payments can be compared by page. A pathname only, as the server accepts
-- it: a slash and up to 200 lowercase letters, digits, '/', '.', '_' and '-'. Older images ignore it.
ALTER TABLE workspace_onboarding
    ADD COLUMN landing_path text CHECK (landing_path ~ '^/[a-z0-9/._-]{0,200}$');
