-- The admin gave beta's invoice article from Cy to Alex; the change is a version of kind author_change.
UPDATE kb_articles SET author_agent_id = md5('agent-beta-alex')::uuid WHERE id = md5('kba-beta-1')::uuid;

INSERT INTO kb_article_versions (id, workspace_id, article_id, version, kind, title, slug, body_md, status, collection_id, changed_by_agent_id,
        author_from_agent_id, author_to_agent_id, created_at) VALUES
    (md5('kba-beta-1-v-author')::uuid, md5('ws-beta')::uuid, md5('kba-beta-1')::uuid,
     (SELECT COALESCE(max(version), 0) + 1 FROM kb_article_versions WHERE article_id = md5('kba-beta-1')::uuid), 'author_change',
     'Download an invoice', 'download-an-invoice', 'Invoices are under Billing.', 'draft', md5('kbc-beta-1')::uuid, md5('agent-beta-admin')::uuid,
     md5('agent-beta-cy')::uuid, md5('agent-beta-alex')::uuid, '2026-11-21 10:00:00+00');
