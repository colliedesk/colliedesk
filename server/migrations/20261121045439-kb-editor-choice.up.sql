-- The article editor each agent writes in; NULL until they choose, and the web app then opens the visual one.
ALTER TABLE agents ADD COLUMN kb_editor text CHECK (kb_editor IN ('visual', 'markdown'));
