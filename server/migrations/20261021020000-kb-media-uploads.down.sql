-- Uploads in progress are only parts of files not stored yet; rolling back drops them, and their files
-- in .incoming go with the attachment storage's cleanup after 24 hours.
DROP TABLE IF EXISTS kb_media_uploads;
ALTER TABLE kb_media DROP COLUMN IF EXISTS poster_content_type;
