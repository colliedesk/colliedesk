-- The tokens a team pastes from Google Search Console and Bing Webmaster Tools to prove it owns its help
-- center host. Each is served as a <meta> tag on that workspace's help center pages only. The server
-- checks the token before a save; the pattern here keeps any other writer to the same shape, which
-- cannot hold a quote, an angle bracket or a space. NULL is none.
ALTER TABLE help_centers
    ADD COLUMN google_site_verification text CHECK (google_site_verification ~ '^[A-Za-z0-9_-]{16,100}$'),
    ADD COLUMN bing_site_verification text CHECK (bing_site_verification ~ '^[A-Za-z0-9_-]{16,100}$');
