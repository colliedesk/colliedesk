-- The stored files the fixture's rows point at, as "storage key<TAB>content" lines. The fixture names
-- its files so their content follows from the row: see the header of the first fixture file.
SELECT to_regclass('mail_image_cache') IS NOT NULL AS has_image_cache \gset
SELECT DISTINCT storage_key, 'colliedesk upgrade fixture: ' || filename FROM attachments ORDER BY 1;
\if :has_image_cache
SELECT storage_key, 'colliedesk upgrade fixture image: ' || url_hash FROM mail_image_cache ORDER BY 1;
\endif
