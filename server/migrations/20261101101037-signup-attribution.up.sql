-- The post or ad a cloud sign-up came from: the utm_* keys of the link and the click IDs TikTok and Meta
-- add to ad links, kept so trials and payments can be traced to it. Values are plain tokens, as the server
-- accepts them; click_id_seen_at is when the browser first saw a click ID (Meta's click time) and is set
-- exactly when there is one. PostgreSQL caps a regex repetition at 255, hence the separate length bound.
-- Older images ignore these columns.
ALTER TABLE workspace_onboarding
    ADD COLUMN utm_source text CHECK (utm_source ~ '^[A-Za-z0-9._-]+$' AND length(utm_source) <= 300),
    ADD COLUMN utm_medium text CHECK (utm_medium ~ '^[A-Za-z0-9._-]+$' AND length(utm_medium) <= 300),
    ADD COLUMN utm_campaign text CHECK (utm_campaign ~ '^[A-Za-z0-9._-]+$' AND length(utm_campaign) <= 300),
    ADD COLUMN utm_content text CHECK (utm_content ~ '^[A-Za-z0-9._-]+$' AND length(utm_content) <= 300),
    ADD COLUMN utm_term text CHECK (utm_term ~ '^[A-Za-z0-9._-]+$' AND length(utm_term) <= 300),
    ADD COLUMN ttclid text CHECK (ttclid ~ '^[A-Za-z0-9._-]+$' AND length(ttclid) <= 300),
    ADD COLUMN fbclid text CHECK (fbclid ~ '^[A-Za-z0-9._-]+$' AND length(fbclid) <= 300),
    ADD COLUMN click_id_seen_at timestamptz,
    ADD CONSTRAINT workspace_onboarding_click_id_seen_at_check
        CHECK ((click_id_seen_at IS NULL) = (ttclid IS NULL AND fbclid IS NULL));
