-- Each agent chooses the columns of the knowledge base article list, in order; null is the default
-- set. The server checks the names, so a name an older image does not know is skipped when read.
ALTER TABLE agents ADD COLUMN kb_list_columns text[];
