-- The brand of a help center: logo and favicon from the media library, the website and contact links,
-- colors and fonts. Null is the default look. The server checks shape and contrast before a save; the
-- checks here keep any other writer to the same shapes. A logo or favicon is an item of the same
-- workspace's library; deleting the item for good clears it here, and nothing else.
ALTER TABLE help_centers
    ADD COLUMN logo_media_id uuid,
    ADD COLUMN favicon_media_id uuid,
    ADD COLUMN logo_alt text NOT NULL DEFAULT '' CHECK (char_length(logo_alt) <= 200),
    ADD COLUMN linkback_url text CHECK (char_length(linkback_url) <= 2000 AND linkback_url ~* '^https?://'),
    ADD COLUMN contact_url text CHECK (char_length(contact_url) <= 2000 AND contact_url ~* '^(https://|mailto:)'),
    ADD COLUMN contact_phone text CHECK (char_length(contact_phone) <= 40 AND contact_phone ~ '^[0-9 +()-]+$'),
    ADD COLUMN color_primary text CHECK (color_primary ~ '^#[0-9a-f]{6}$'),
    ADD COLUMN color_header text CHECK (color_header ~ '^#[0-9a-f]{6}$'),
    ADD COLUMN color_header_text text CHECK (color_header_text ~ '^#[0-9a-f]{6}$'),
    ADD COLUMN color_background text CHECK (color_background ~ '^#[0-9a-f]{6}$'),
    ADD COLUMN color_text text CHECK (color_text ~ '^#[0-9a-f]{6}$'),
    ADD COLUMN color_link text CHECK (color_link ~ '^#[0-9a-f]{6}$'),
    ADD COLUMN color_footer text CHECK (color_footer ~ '^#[0-9a-f]{6}$'),
    ADD COLUMN body_font text CHECK (body_font IN ('system-sans', 'humanist-sans', 'geometric-sans', 'classic-serif',
                                                   'transitional-serif', 'rounded-sans', 'mono')),
    ADD COLUMN heading_font text CHECK (heading_font IN ('system-sans', 'humanist-sans', 'geometric-sans', 'classic-serif',
                                                         'transitional-serif', 'rounded-sans', 'mono')),
    ADD CONSTRAINT help_centers_logo_media_fkey FOREIGN KEY (workspace_id, logo_media_id)
        REFERENCES kb_media(workspace_id, id) ON DELETE SET NULL (logo_media_id),
    ADD CONSTRAINT help_centers_favicon_media_fkey FOREIGN KEY (workspace_id, favicon_media_id)
        REFERENCES kb_media(workspace_id, id) ON DELETE SET NULL (favicon_media_id);

-- A purge of library items looks up the help center that names each one.
CREATE INDEX help_centers_logo_media_idx ON help_centers(workspace_id, logo_media_id) WHERE logo_media_id IS NOT NULL;
CREATE INDEX help_centers_favicon_media_idx ON help_centers(workspace_id, favicon_media_id) WHERE favicon_media_id IS NOT NULL;
