-- acme's admin chose the knowledge base list columns; everyone else keeps the default (NULL).
UPDATE agents SET kb_list_columns = ARRAY['status', 'tags', 'updated'] WHERE id = md5('agent-acme-admin')::uuid;
