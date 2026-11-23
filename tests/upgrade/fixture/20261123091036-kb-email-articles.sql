-- acme and beta each remember the forwarded mail that made one of their articles, so the same mail
-- delivered again adds no second draft.
INSERT INTO kb_email_articles (workspace_id, message_id, article_id, created_at) VALUES
    (md5('ws-acme')::uuid, 'forward-acme-1@mail.acme.example', md5('kba-acme-1')::uuid,
        '2026-11-23 09:00:00+00'),
    (md5('ws-beta')::uuid, 'forward-beta-1@mail.beta.example', md5('kba-beta-1')::uuid,
        '2026-11-23 09:30:00+00');
