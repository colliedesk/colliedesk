-- acme ordered its articles by hand; beta never did, so its articles keep position 0 and the title order.
UPDATE kb_articles SET position = 1 WHERE id = md5('kba-acme-1')::uuid;
