-- Translations become language versions: acme publishes its German translation, which a person then
-- edits through an agent token (and restores), while the article changed after it (outdated); beta
-- translates its invoice article into Portuguese and has a Spanish one in the trash. Both name their
-- collections in another language and list their languages (acme shows German).
UPDATE kb_article_translations SET status = 'published', published_at = '2024-02-03 10:00:00+00', author_agent_id = md5('agent-acme-ben')::uuid,
    outdated = true, outdated_at = '2024-03-01 10:00:00+00'
    WHERE workspace_id = md5('ws-acme')::uuid AND article_id = md5('kba-acme-1')::uuid AND locale = 'de';
INSERT INTO kb_article_translation_versions (id, workspace_id, article_id, locale, version, kind, restored_from_version, title, slug, body_md, seo_title, seo_description, status,
        changed_by_agent_id, token_id, token_label, agent_request_id, created_at) VALUES
    (md5('kbt-acme-de-v2')::uuid, md5('ws-acme')::uuid, md5('kba-acme-1')::uuid, 'de', 2, 'restore', 1, 'Passwort zurücksetzen', 'passwort-zurücksetzen',
     'Öffnen Sie **Einstellungen**.', '', '', 'published', md5('agent-acme-ben')::uuid, md5('token-acme-read')::uuid, 'Claude (read)', NULL, '2024-03-02 10:00:00+00');

INSERT INTO kb_article_translations (workspace_id, article_id, locale, title, slug, body_md, status, published_at, seo_title, seo_description, author_agent_id,
        created_at, updated_at, deleted_at, deleted_by_agent_id) VALUES
    (md5('ws-beta')::uuid, md5('kba-beta-1')::uuid, 'pt-BR', 'Baixar uma fatura', 'baixar-uma-fatura', 'As faturas ficam em Cobrança.', 'draft', NULL, 'Fatura', '',
     md5('agent-beta-cy')::uuid, '2026-11-05 09:00:00+00', '2026-11-05 09:00:00+00', NULL, NULL),
    (md5('ws-beta')::uuid, md5('kba-beta-1')::uuid, 'es', 'Descargar una factura', 'descargar-una-factura', 'Las facturas están en Facturación.', 'draft', NULL, '', '',
     md5('agent-beta-cy')::uuid, '2026-11-05 09:00:00+00', now() - interval '1 day', now() - interval '1 day', md5('agent-beta-admin')::uuid);
INSERT INTO kb_article_translation_versions (id, workspace_id, article_id, locale, version, kind, title, slug, body_md, status, changed_by_agent_id, token_id, token_label,
        agent_request_id, created_at) VALUES
    (md5('kbt-beta-pt-v1')::uuid, md5('ws-beta')::uuid, md5('kba-beta-1')::uuid, 'pt-BR', 1, 'create', 'Baixar uma fatura', 'baixar-uma-fatura',
     'As faturas ficam em Cobrança.', 'draft', md5('agent-beta-cy')::uuid, md5('token-beta-send')::uuid, 'Claude (send)', md5('request-beta-saved-reply')::uuid, '2026-11-05 09:00:00+00'),
    (md5('kbt-beta-es-v1')::uuid, md5('ws-beta')::uuid, md5('kba-beta-1')::uuid, 'es', 1, 'create', 'Descargar una factura', 'descargar-una-factura',
     'Las facturas están en Facturación.', 'draft', md5('agent-beta-cy')::uuid, NULL, NULL, NULL, '2026-11-05 09:00:00+00'),
    (md5('kbt-beta-es-v2')::uuid, md5('ws-beta')::uuid, md5('kba-beta-1')::uuid, 'es', 2, 'delete', 'Descargar una factura', 'descargar-una-factura',
     'Las facturas están en Facturación.', 'draft', md5('agent-beta-admin')::uuid, NULL, NULL, NULL, now() - interval '1 day');

INSERT INTO kb_collection_translations (workspace_id, collection_id, locale, name, description, source_meta, created_at, updated_at) VALUES
    (md5('ws-acme')::uuid, md5('kbc-acme-1')::uuid, 'de', 'Erste Schritte', 'Die ersten Schritte', '{"freshdesk": "c101", "language": "de"}', '2024-02-01 10:00:00+00', '2024-02-01 10:00:00+00'),
    (md5('ws-beta')::uuid, md5('kbc-beta-1')::uuid, 'pt-BR', 'Cobrança', '', NULL, '2026-11-05 09:00:00+00', '2026-11-05 09:00:00+00');

INSERT INTO help_center_languages (workspace_id, locale, visible, position, created_at, updated_at) VALUES
    (md5('ws-acme')::uuid, 'de', true, 1, '2024-02-03 10:00:00+00', '2026-11-05 09:00:00+00')
    ON CONFLICT (workspace_id, locale) DO UPDATE SET visible = true, position = 1;
INSERT INTO help_center_languages (workspace_id, locale, visible, position, created_at, updated_at) VALUES
    (md5('ws-beta')::uuid, 'es', false, 2, '2026-11-05 09:00:00+00', '2026-11-05 09:00:00+00');
