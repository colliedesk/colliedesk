-- Remote images of received mail, cached. A file holds 'colliedesk upgrade fixture image: ' || url_hash.
INSERT INTO mail_image_cache (workspace_id, url_hash, storage_key, content_type, size, fetched_at, last_served_at)
SELECT md5('ws-beta')::uuid, h, md5('ws-beta')::uuid || '/' || substr(s, 1, 2) || '/' || s, 'image/png',
       octet_length(convert_to('colliedesk upgrade fixture image: ' || h, 'UTF8')), '2026-10-18 17:00:00+00', '2026-10-18 18:00:00+00'
FROM (SELECT h, encode(sha256(convert_to('colliedesk upgrade fixture image: ' || h, 'UTF8')), 'hex') AS s
      FROM (SELECT encode(sha256(convert_to('https://images.beta-users.example/' || i || '.png', 'UTF8')), 'hex') AS h
            FROM generate_series(1, 2) AS i) AS urls) AS files;
