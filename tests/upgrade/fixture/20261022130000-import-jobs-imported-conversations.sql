-- acme's finished import counted its conversations when it ended.
UPDATE import_jobs SET imported_conversations = 300 WHERE id = md5('ij-acme-1')::uuid;
