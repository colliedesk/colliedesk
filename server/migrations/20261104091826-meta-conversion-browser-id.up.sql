-- The browser ID the Meta pixel keeps in the _fbp cookie on colliedesk.com, read from the sign-up request
-- when the visitor accepted cookies on the website, so Meta can match a report from an ad to its visit. Only
-- Meta's own format is stored; it is cleared with the sign-up's address and browser. Older images ignore it.
ALTER TABLE meta_conversion_events
    ADD COLUMN fbp text CHECK (fbp ~ '^fb\.[0-9]\.[0-9]+\.[0-9]+$' AND length(fbp) <= 64);
