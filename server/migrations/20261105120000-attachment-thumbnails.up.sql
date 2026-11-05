-- The thumbnail the web app shows for a picture attachment (GET /api/v1/attachments/{id}/thumbnail), made on
-- the first request and kept: NULL until someone asks, '' when the file is no picture the server can read.
ALTER TABLE attachments ADD COLUMN thumb_storage_key text;
CREATE INDEX attachments_thumb_key_idx ON attachments(workspace_id, thumb_storage_key) WHERE thumb_storage_key <> '';
