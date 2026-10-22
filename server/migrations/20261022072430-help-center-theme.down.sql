-- An older image shows the default look again; the theme set since the upgrade is lost. The library
-- items it named stay, with the kept mark the theme gave them.
DROP INDEX IF EXISTS help_centers_favicon_media_idx;
DROP INDEX IF EXISTS help_centers_logo_media_idx;
ALTER TABLE help_centers
    DROP CONSTRAINT IF EXISTS help_centers_favicon_media_fkey,
    DROP CONSTRAINT IF EXISTS help_centers_logo_media_fkey,
    DROP COLUMN IF EXISTS heading_font,
    DROP COLUMN IF EXISTS body_font,
    DROP COLUMN IF EXISTS color_footer,
    DROP COLUMN IF EXISTS color_link,
    DROP COLUMN IF EXISTS color_text,
    DROP COLUMN IF EXISTS color_background,
    DROP COLUMN IF EXISTS color_header_text,
    DROP COLUMN IF EXISTS color_header,
    DROP COLUMN IF EXISTS color_primary,
    DROP COLUMN IF EXISTS contact_phone,
    DROP COLUMN IF EXISTS contact_url,
    DROP COLUMN IF EXISTS linkback_url,
    DROP COLUMN IF EXISTS logo_alt,
    DROP COLUMN IF EXISTS favicon_media_id,
    DROP COLUMN IF EXISTS logo_media_id;
