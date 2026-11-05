-- Attachment thumbnails: acme's first logo.png was viewed, so the server stored its thumbnail, a file
-- holding 'colliedesk upgrade fixture thumbnail: ' || filename; invoice-6.pdf was asked for one and is
-- marked as having none (''); every other attachment was never asked (NULL).
UPDATE attachments a SET thumb_storage_key = a.workspace_id || '/' || left(h, 2) || '/' || h
FROM (SELECT encode(sha256(convert_to('colliedesk upgrade fixture thumbnail: logo.png', 'UTF8')), 'hex') AS h) s
WHERE a.workspace_id = md5('ws-acme')::uuid AND a.id = md5('att-acme-1')::uuid;
UPDATE attachments SET thumb_storage_key = '' WHERE workspace_id = md5('ws-acme')::uuid AND id = md5('att-acme-6')::uuid;
