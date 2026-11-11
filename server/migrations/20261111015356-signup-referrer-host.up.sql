-- The site that sent a cloud sign-up to the sign-in page, when its link had no utm_* keys to say so (an AI
-- assistant such as claude.ai links straight to the app). Only the lowercase host name is kept, never a path
-- or a query, so the value cannot carry what a person searched for or typed. Older images ignore the column.
ALTER TABLE workspace_onboarding
    ADD COLUMN referrer_host text CHECK (referrer_host ~ '^[a-z0-9.-]+$' AND length(referrer_host) <= 253);
