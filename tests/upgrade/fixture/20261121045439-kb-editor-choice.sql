-- acme's admin writes articles in Markdown and beta's admin chose the visual editor; everyone else never
-- chose. An upgrade must keep each person's choice.
UPDATE agents SET kb_editor = 'markdown' WHERE id = md5('agent-acme-admin')::uuid;
UPDATE agents SET kb_editor = 'visual' WHERE id = md5('agent-beta-admin')::uuid;
