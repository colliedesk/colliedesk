-- acme lists its password article (home: Accounts) in Getting started too.
INSERT INTO kb_article_collections (workspace_id, article_id, collection_id, created_at) VALUES
    (md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, md5('kbc-acme-1')::uuid, '2026-11-06 04:00:00+00');
